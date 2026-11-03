/* Landing on the episode a search hit named (`?ep=`): bring that row to the
   middle of the screen, and only then flash it, so the pulses do not burn while
   the page is still travelling.

   The travel is fragile. Every programmatic scroll starts by aborting whatever
   smooth scroll is already running (CSSOM: "Abort any ongoing smooth scroll for
   box"), so anything that scrolls the window a frame later leaves the page
   short of the row. Telling "arrived" from "stopped moving" is not enough
   either: a cancelled smooth scroll stops moving immediately. So the row itself
   is checked, and put in place once without animation if a smooth scroll did not
   get there. */

function frame(signal) {
  return new Promise((resolve) => {
    if (signal?.aborted) return resolve(false)
    const onAbort = () => { cancelAnimationFrame(handle); resolve(false) }
    const handle = requestAnimationFrame(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve(true)
    })
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

/* What scrollIntoView({ block: 'center' }) is aiming at. A row taller than the
   viewport can never be centred, so it only has to cover the screen instead. */
function centred(element) {
  const box = element.getBoundingClientRect()
  const height = window.innerHeight || document.documentElement.clientHeight
  if (box.height > height) return box.top <= 0 && box.bottom >= height
  const middle = box.top + box.height / 2
  return middle >= 0 && middle <= height
}

/* Wait out the scroll, then report whether the row is where it was asked to be.
   120ms without movement means the animation finished — or was cancelled before
   it went anywhere, which the position check is there to notice. */
async function settled(element, signal, timeout) {
  const start = performance.now()
  let lastTop = null
  let stillSince = start
  for (;;) {
    if (!(await frame(signal)) || !element.isConnected) return false
    const top = element.getBoundingClientRect().top
    const now = performance.now()
    if (lastTop === null || Math.abs(top - lastTop) > 0.5) stillSince = now
    lastTop = top
    if (now - stillSince >= 120 || now - start >= timeout) return centred(element)
  }
}

export async function scrollToEpisode(element, { signal, reducedMotion = globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false } = {}) {
  if (signal?.aborted || !element.isConnected) return false
  // Rows mount with the staggered card entrance, which only moves them by their
  // own transform: one frame and the layout they are measured in is final.
  if (!(await frame(signal)) || !element.isConnected) return false
  element.scrollIntoView({ block: 'center', inline: 'nearest', behavior: reducedMotion ? 'auto' : 'smooth' })
  if (await settled(element, signal, 2000)) return true
  // The smooth scroll was cut short by something else. Place the row once,
  // without animation, and report what actually happened — the caller flashes
  // only on a true.
  element.scrollIntoView({ block: 'center', inline: 'nearest', behavior: 'auto' })
  return settled(element, signal, 600)
}
