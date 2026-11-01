package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"doriyamedia/internal/auth"
	"doriyamedia/internal/config"
	"doriyamedia/internal/db"
	"doriyamedia/internal/models"
)

// Account rules mirrored from the API so a CLI-created account is exactly one
// the sign-in form would have accepted.
const (
	minNameLen    = 2
	maxNameLen    = 32
	maxPasswordLn = 200
)

var nameRe = regexp.MustCompile(`^[\p{L}\p{N}_.@-]+$`)

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch n := utf8.RuneCountInString(name); {
	case n < minNameLen || n > maxNameLen:
		return "", fmt.Errorf("用户名需要 %d-%d 个字符", minNameLen, maxNameLen)
	case !nameRe.MatchString(name):
		return "", fmt.Errorf("用户名只能包含中英文、数字与 _ - . @")
	}
	return name, nil
}

func validatePassword(cfg *config.Config, pw string) error {
	min := cfg.Users.MinPassword
	if min < config.DefaultMinPassword {
		min = config.DefaultMinPassword
	}
	if utf8.RuneCountInString(pw) < min {
		return fmt.Errorf("密码至少 %d 位", min)
	}
	if len(pw) > maxPasswordLn {
		return fmt.Errorf("密码过长(最多 %d 字节)", maxPasswordLn)
	}
	return nil
}

func normalizeRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), models.RoleAdmin) {
		return models.RoleAdmin
	}
	return models.RoleUser
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case models.StatusDisabled, models.StatusPending:
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return models.StatusActive
	}
}

// runUser manages accounts.
func runUser(a *App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: doriyamedia user <list|add|passwd|role|status|rm> ...")
	}
	switch args[0] {
	case "list", "ls":
		return userList(a, args[1:])
	case "add":
		return userAdd(a, args[1:])
	case "passwd", "password":
		return userPasswd(a, args[1:])
	case "role":
		return userRole(a, args[1:])
	case "status":
		return userStatus(a, args[1:])
	case "rm", "delete":
		return userRemove(a, args[1:])
	default:
		return fmt.Errorf("未知子命令: user %s", args[0])
	}
}

func userList(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	jsonOut := fs.Bool("json", a.JSON, "以 JSON 输出")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a.JSON = *jsonOut
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		users, err := store.ListUsers()
		if err != nil {
			return err
		}
		return a.emit(users, func() {
			if len(users) == 0 {
				fmt.Fprintln(a.Out, "还没有账号。用 doriyamedia user add --admin 创建管理员。")
				return
			}
			rows := make([][]string, 0, len(users))
			for _, u := range users {
				last := "-"
				if u.LastLogin != nil {
					last = humanTime(*u.LastLogin)
				}
				rows = append(rows, []string{
					fmt.Sprint(u.ID), u.Name, u.Role, u.Status, u.Email, last,
				})
			}
			a.table([]string{"ID", "用户名", "角色", "状态", "邮箱", "最后登录"}, rows)
		})
	})
}

func userAdd(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	name := fs.String("name", "", "用户名")
	password := fs.String("password", "", "密码（省略则交互输入）")
	admin := fs.Bool("admin", false, "创建管理员")
	email := fs.String("email", "", "邮箱（可选）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("多余的参数: %v", fs.Args())
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		userName, err := validateName(*name)
		if err != nil {
			return err
		}
		pw := *password
		if pw == "" {
			pw, err = readPassword(a, "密码: ")
			if err != nil {
				return err
			}
			confirm, err := readPassword(a, "再输一次: ")
			if err != nil {
				return err
			}
			if pw != confirm {
				return fmt.Errorf("两次输入的密码不一致")
			}
		}
		if err := validatePassword(cfg, pw); err != nil {
			return err
		}
		taken, err := store.NameTaken(userName, 0)
		if err != nil {
			return err
		}
		if taken {
			return fmt.Errorf("用户名 %q 已被使用", userName)
		}
		if *email != "" {
			if taken, err := store.EmailTaken(*email, 0); err != nil {
				return err
			} else if taken {
				return fmt.Errorf("邮箱 %q 已被使用", *email)
			}
		}
		role := models.RoleUser
		if *admin {
			role = models.RoleAdmin
		}
		u := &models.User{
			Name: userName, Role: role, Status: models.StatusActive,
			Email: strings.TrimSpace(*email),
			// An address typed by an operator is trusted: there is no mail round
			// trip to prove it, and leaving it unverified would lock the account
			// behind the verification gate.
			EmailVerified: strings.TrimSpace(*email) != "",
		}
		u.PassHash, u.Salt = auth.HashPassword(pw)
		if err := store.CreateUser(u); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(u, nil)
		}
		fmt.Fprintf(a.Out, "已创建账号 %s（ID %d，角色 %s）\n", u.Name, u.ID, u.Role)
		return nil
	})
}

func userPasswd(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	id := fs.Int64("id", 0, "账号 ID")
	name := fs.String("name", "", "用户名（代替 --id）")
	password := fs.String("password", "", "新密码（省略则交互输入）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		u, err := resolveUser(store, *id, *name)
		if err != nil {
			return err
		}
		pw := *password
		if pw == "" {
			pw, err = readPassword(a, "新密码: ")
			if err != nil {
				return err
			}
			confirm, err := readPassword(a, "再输一次: ")
			if err != nil {
				return err
			}
			if pw != confirm {
				return fmt.Errorf("两次输入的密码不一致")
			}
		}
		if err := validatePassword(cfg, pw); err != nil {
			return err
		}
		hash, salt := auth.HashPassword(pw)
		if err := store.UpdateUserPassword(u.ID, hash, salt); err != nil {
			return err
		}
		// Existing sessions were issued against the old password; dropping them
		// is what makes a reset actually take effect on a device already signed
		// in (the same thing the admin UI does).
		if err := store.DeleteUserSessions(u.ID, ""); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "user_id": u.ID}, nil)
		}
		fmt.Fprintf(a.Out, "已重置 %s 的密码，并使其已登录的会话失效\n", u.Name)
		return nil
	})
}

func userRole(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	id := fs.Int64("id", 0, "账号 ID")
	name := fs.String("name", "", "用户名（代替 --id）")
	role := fs.String("role", "", "admin 或 user")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*role) == "" {
		return fmt.Errorf("需要 --role admin|user")
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		u, err := resolveUser(store, *id, *name)
		if err != nil {
			return err
		}
		next := normalizeRole(*role)
		if u.Role == models.RoleAdmin && next != models.RoleAdmin {
			if err := guardLastAdmin(store, u); err != nil {
				return err
			}
		}
		if err := store.UpdateUserRole(u.ID, next); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "user_id": u.ID, "role": next}, nil)
		}
		fmt.Fprintf(a.Out, "已把 %s 的角色改为 %s\n", u.Name, next)
		return nil
	})
}

func userStatus(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	id := fs.Int64("id", 0, "账号 ID")
	name := fs.String("name", "", "用户名（代替 --id）")
	status := fs.String("status", "", "active|pending|disabled")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*status) == "" {
		return fmt.Errorf("需要 --status active|pending|disabled")
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		u, err := resolveUser(store, *id, *name)
		if err != nil {
			return err
		}
		next := normalizeStatus(*status)
		if u.Role == models.RoleAdmin && next != models.StatusActive {
			if err := guardLastAdmin(store, u); err != nil {
				return err
			}
		}
		if err := store.UpdateUserStatus(u.ID, next); err != nil {
			return err
		}
		if next != models.StatusActive {
			_ = store.DeleteUserSessions(u.ID, "")
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "user_id": u.ID, "status": next}, nil)
		}
		fmt.Fprintf(a.Out, "已把 %s 的状态改为 %s\n", u.Name, next)
		return nil
	})
}

func userRemove(a *App, args []string) error {
	fs := newFlagSet(a, "user")
	id := fs.Int64("id", 0, "账号 ID")
	name := fs.String("name", "", "用户名（代替 --id）")
	yes := fs.Bool("yes", false, "跳过确认")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return a.withStore(func(cfg *config.Config, store *db.Store) error {
		u, err := resolveUser(store, *id, *name)
		if err != nil {
			return err
		}
		if u.Role == models.RoleAdmin {
			if err := guardLastAdmin(store, u); err != nil {
				return err
			}
		}
		if !*yes {
			return fmt.Errorf("删除账号 %q 会一并删除它的播放进度；确认请加 --yes", u.Name)
		}
		if err := store.DeleteUser(u.ID); err != nil {
			return err
		}
		if a.JSON {
			return a.emit(map[string]any{"ok": true, "deleted": u.ID}, nil)
		}
		fmt.Fprintf(a.Out, "已删除账号 %s（ID %d）\n", u.Name, u.ID)
		return nil
	})
}

// guardLastAdmin refuses to remove the only usable administrator, which would
// leave the server with no way into its settings.
func guardLastAdmin(store *db.Store, u *models.User) error {
	if u.Status != models.StatusActive {
		return nil
	}
	n, err := store.ActiveAdminCount()
	if err != nil {
		return err
	}
	if n <= 1 {
		return errors.New("这是最后一个可用管理员，请先创建或启用另一个管理员账号")
	}
	return nil
}

// resolveUser finds one account by ID or exact (case-insensitive) name.
func resolveUser(store *db.Store, id int64, name string) (*models.User, error) {
	if id > 0 {
		u, err := store.UserByID(id)
		if err != nil {
			return nil, fmt.Errorf("账号 %d 不存在", id)
		}
		return u, nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("需要 --id 或 --name 指定账号")
	}
	u, err := store.UserByName(name)
	if err != nil {
		return nil, fmt.Errorf("账号 %q 不存在", name)
	}
	return u, nil
}

// readPassword reads a password without echoing it when stdin is a terminal,
// and falls back to a plain line read when it is piped (a script, CI).
func readPassword(a *App, prompt string) (string, error) {
	fmt.Fprint(a.Out, prompt)
	pw, err := readSecretLine(os.Stdin)
	fmt.Fprintln(a.Out)
	if err != nil {
		return "", err
	}
	pw = strings.TrimRight(pw, "\r\n")
	if pw == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	return pw, nil
}

// readSecretLine disables echo on a terminal. Piped input is read normally.
func readSecretLine(f *os.File) (string, error) {
	state, ok := disableEcho(f)
	if !ok {
		r := bufio.NewReader(f)
		return r.ReadString('\n')
	}
	defer restoreEcho(f, state)
	r := bufio.NewReader(f)
	return r.ReadString('\n')
}
