'use strict'

const DEFAULT_SIDEBAR_WIDTH = 288
const MIN_SIDEBAR_WIDTH = 240
const MAX_SIDEBAR_WIDTH = 720
const KEYBOARD_RESIZE_STEP = 16
const SIDEBAR_WIDTH_PROPERTY = '--serveo-sidebar-width'
const SIDEBAR_WIDTH_STORAGE_KEY = 'serveo.sidebar-width'

const app = document.querySelector('#app')
const configuration = JSON.parse(app.dataset.configuration)
let preferredSidebarWidth = readStoredSidebarWidth()
let resizer

applySidebarWidth()
Scalar.createApiReference('#app', configuration)

resizer = document.createElement('div')
resizer.className = 'serveo-sidebar-resizer'
resizer.setAttribute('aria-controls', 'app')
resizer.setAttribute('aria-label', 'Resize API navigation sidebar')
resizer.setAttribute('aria-orientation', 'vertical')
resizer.setAttribute('role', 'separator')
resizer.tabIndex = 0
document.body.append(resizer)
applySidebarWidth()

let pointerID
let pointerStart
let widthStart

resizer.addEventListener('pointerdown', (event) => {
  if (event.button !== 0 || pointerID !== undefined) {
    return
  }

  event.preventDefault()
  pointerID = event.pointerId
  pointerStart = event.clientX
  widthStart = displayedSidebarWidth()
  resizer.setPointerCapture(pointerID)
  document.body.classList.add('serveo-sidebar-resizing')
})

resizer.addEventListener('pointermove', (event) => {
  if (event.pointerId !== pointerID) {
    return
  }

  const direction = document.documentElement.dir === 'rtl' ? -1 : 1
  preferredSidebarWidth = clampDisplayedWidth(widthStart + (event.clientX - pointerStart) * direction)
  applySidebarWidth()
})

for (const eventName of ['pointerup', 'pointercancel', 'lostpointercapture']) {
  resizer.addEventListener(eventName, finishPointerResize)
}

resizer.addEventListener('keydown', (event) => {
  const direction = document.documentElement.dir === 'rtl' ? -1 : 1
  const displayedWidth = displayedSidebarWidth()
  let nextWidth

  if (event.key === 'ArrowLeft') {
    nextWidth = displayedWidth - KEYBOARD_RESIZE_STEP * direction
  } else if (event.key === 'ArrowRight') {
    nextWidth = displayedWidth + KEYBOARD_RESIZE_STEP * direction
  } else if (event.key === 'Home') {
    nextWidth = DEFAULT_SIDEBAR_WIDTH
  } else {
    return
  }

  event.preventDefault()
  preferredSidebarWidth = clampDisplayedWidth(nextWidth)
  applySidebarWidth()
  storeSidebarWidth()
})

resizer.addEventListener('dblclick', () => {
  preferredSidebarWidth = DEFAULT_SIDEBAR_WIDTH
  applySidebarWidth()
  storeSidebarWidth()
})

window.addEventListener('resize', applySidebarWidth)

function finishPointerResize(event) {
  if (event.pointerId !== pointerID) {
    return
  }

  pointerID = undefined
  document.body.classList.remove('serveo-sidebar-resizing')
  storeSidebarWidth()
}

function applySidebarWidth() {
  const maximum = viewportMaximumWidth()
  const displayedWidth = displayedSidebarWidth()

  document.documentElement.style.setProperty(SIDEBAR_WIDTH_PROPERTY, `${displayedWidth}px`)
  if (resizer) {
    resizer.setAttribute('aria-valuemax', String(maximum))
    resizer.setAttribute('aria-valuemin', String(MIN_SIDEBAR_WIDTH))
    resizer.setAttribute('aria-valuenow', String(Math.round(displayedWidth)))
  }
}

function clampPreferredWidth(width) {
  return Math.min(Math.max(width, MIN_SIDEBAR_WIDTH), MAX_SIDEBAR_WIDTH)
}

function clampDisplayedWidth(width) {
  return Math.min(clampPreferredWidth(width), viewportMaximumWidth())
}

function displayedSidebarWidth() {
  return Math.min(preferredSidebarWidth, viewportMaximumWidth())
}

function viewportMaximumWidth() {
  return Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, Math.floor(window.innerWidth / 2)))
}

function readStoredSidebarWidth() {
  try {
    const storedWidth = Number.parseFloat(window.sessionStorage.getItem(SIDEBAR_WIDTH_STORAGE_KEY))
    return Number.isFinite(storedWidth) ? clampPreferredWidth(storedWidth) : DEFAULT_SIDEBAR_WIDTH
  } catch {
    return DEFAULT_SIDEBAR_WIDTH
  }
}

function storeSidebarWidth() {
  try {
    window.sessionStorage.setItem(SIDEBAR_WIDTH_STORAGE_KEY, String(Math.round(preferredSidebarWidth)))
  } catch {
    // Resizing still works when browser storage is unavailable.
  }
}
