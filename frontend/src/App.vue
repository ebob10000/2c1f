<script setup>
import {ref, onMounted, computed, reactive, nextTick} from 'vue'
import {SelectFile, SelectFolder, SelectSaveDirectory, StartSender, StartReceiver, GetSettings, SaveSettings, CancelTransfer, CopyToClipboard, GetTransferHistory, GetVersion, DownloadAndInstallUpdate} from '../wailsjs/go/main/App'
import {EventsOn, WindowMinimise, WindowToggleMaximise, Quit} from '../wailsjs/runtime'

const mode = ref('send')
const errorMsg = ref('')

// Update notification state
const updateAvailable = ref(null)
const updateDownloading = ref(false)
const updateProgress = ref(0)
const updateDismissed = ref(false)
const appVersion = ref('')

// Global Settings
const settings = reactive({
  autoHash: true,
  compress: false,
  cacheManifest: true
})

// Console Logs
const consoleLogs = ref([])
const logContainer = ref(null)
const consoleCollapsed = ref(true)
const consolePanelHeight = ref(240)
const isResizing = ref(false)

function addLog(msg, type = 'info') {
  // Filter out unnecessary system logs
  const skipLogs = ['Loading settings...', 'Settings loaded', 'Application mounted', 'Navigated to', 'Resetting application state', 'Updating settings...', 'Selected file:', 'Selected folder:', 'Selected destination:']
  if (skipLogs.some(skip => msg.includes(skip))) return

  const time = new Date().toLocaleTimeString('en-US', {hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit'})
  consoleLogs.value.push({ time, msg, type })
  if (consoleLogs.value.length > 100) consoleLogs.value.shift()
  nextTick(() => {
    if (logContainer.value) logContainer.value.scrollTop = logContainer.value.scrollHeight
  })
}

async function loadSettings() {
  addLog('Loading settings...', 'system')
  const s = await GetSettings()
  if (s) {
    Object.assign(settings, s)
    addLog('Settings loaded', 'success')
  }
}

function updateSettings() {
  addLog('Updating settings...', 'system')
  SaveSettings(JSON.parse(JSON.stringify(settings)))
}

// Sender State
const sendPath = ref('')
const sendCode = ref('')
const isSending = ref(false)
const isConnecting = ref(false)
const senderStatus = ref('Starting...')
const loadingPhase = ref('') // Specific loading phase

// File Tree State & Progress
const manifestFiles = ref([]) // Now holds objects with {path, size, progress}
const completedFiles = reactive(new Set())
const hashingFile = ref('')
const hashingProgress = ref({ current: 0, total: 0 })
const transferName = ref('') // Name of folder/file being transferred

// Receiver State
const recvCode = ref('')
const destPath = ref('')
const fastResume = ref(false)
const isReceiving = ref(false)

const transferSpeed = ref(0)
const transferComplete = ref(false)
const etaSeconds = ref(0)
const codeCopied = ref(false)

// Transfer History
const transferHistory = ref([])
const isDragging = ref(false)

// File Progress State
const currentFile = ref('')
const fileProgressPercent = ref(0)
const globalProgressPercent = ref(0)
const globalSent = ref(0)
const globalTotal = ref(0)

let lastBytes = 0
let lastTime = 0
let transferStartTime = 0
let speedInterval = null

function resetState() {
  addLog('Resetting application state', 'system')
  errorMsg.value = ''
  globalProgressPercent.value = 0
  globalSent.value = 0
  globalTotal.value = 0
  fileProgressPercent.value = 0
  currentFile.value = ''
  transferSpeed.value = 0
  etaSeconds.value = 0
  transferComplete.value = false
  lastBytes = 0
  lastTime = Date.now()
  manifestFiles.value = []
  completedFiles.clear()
  hashingFile.value = ''
  hashingProgress.value = { current: 0, total: 0 }
  loadingPhase.value = ''
  sendCode.value = ''
  isConnecting.value = false
  isSending.value = false
  isReceiving.value = false
  if (speedInterval) clearInterval(speedInterval)
}

function startSpeedometer() {
  lastTime = Date.now()
  transferStartTime = Date.now()
  lastBytes = 0
  speedInterval = setInterval(() => {
    const now = Date.now()
    const diffTime = (now - lastTime) / 1000
    if (diffTime >= 1) {
      const diffBytes = globalSent.value - lastBytes
      transferSpeed.value = diffBytes / diffTime
      const remainingBytes = globalTotal.value - globalSent.value
      etaSeconds.value = transferSpeed.value > 0 ? remainingBytes / transferSpeed.value : 0
      lastBytes = globalSent.value
      lastTime = now
    }
  }, 1000)
}

function formatSize(bytes) {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
}

function formatTime(seconds) {
  if (seconds === Infinity || seconds === 0) return '--'
  if (seconds < 60) return Math.round(seconds) + 's'
  const mins = Math.floor(seconds / 60)
  if (mins < 60) return mins + 'm ' + Math.round(seconds % 60) + 's'
  const hours = Math.floor(mins / 60)
  return hours + 'h ' + (mins % 60) + 'm'
}

const formattedSpeed = computed(() => formatSize(transferSpeed.value) + '/s')
const formattedTransferred = computed(() => formatSize(globalSent.value))
const formattedTotal = computed(() => formatSize(globalTotal.value))
const formattedEta = computed(() => formatTime(etaSeconds.value))

// Initialize file object in manifest list
function updateManifestProgress(filename, percent) {
  const file = manifestFiles.value.find(f => f.path === filename)
  if (file) {
    file.progress = percent
  }
}

onMounted(() => {
  addLog('Application mounted', 'system')
  loadSettings()
  
  EventsOn("error", (msg) => {
    addLog(`Error: ${msg}`, 'error')
    errorMsg.value = msg; isSending.value = false; isReceiving.value = false; isConnecting.value = false
    if (speedInterval) clearInterval(speedInterval)
  })
  
  EventsOn("sender_status", (msg) => {
    senderStatus.value = msg
    // Set specific loading phases based on status
    if (msg.includes('Initializing')) loadingPhase.value = 'init'
    else if (msg.includes('P2P node')) loadingPhase.value = 'p2p'
    else if (msg.includes('Waiting')) loadingPhase.value = 'waiting'
    addLog(msg, 'info')
  })

  EventsOn("hashing_progress", (data) => {
    hashingFile.value = data.filename
    loadingPhase.value = 'hashing'
    // Count files being hashed
    if (!hashingProgress.value.total) {
      hashingProgress.value.total = manifestFiles.value.length || 1
    }
    hashingProgress.value.current++
  })

  EventsOn("sender_ready", (code) => {
    sendCode.value = code
    hashingFile.value = ''
    loadingPhase.value = 'ready'
    addLog(`Connection code generated: ${code}`, 'success')
  })

  EventsOn("transfer_manifest", (data) => {
    // Map backend files to UI objects with progress
    manifestFiles.value = data.files.map(f => ({...f, progress: 0}))
    hashingProgress.value.total = data.files.length
    transferName.value = data.folderName || 'Files'
    addLog(`Transfer prepared: ${data.files.length} file${data.files.length !== 1 ? 's' : ''} (${formatSize(data.totalSize)} total)`, 'info')
  })
  
  EventsOn("transfer_start_file", (data) => {
    currentFile.value = data.filename
    addLog(`[${data.index}/${data.total}] Transferring: ${data.filename}`, 'info')
    if (!isSending.value && !isReceiving.value) {
       isConnecting.value = false
       if (mode.value === 'send') isSending.value = true; else isReceiving.value = true
       startSpeedometer()
    }
  })
  
  EventsOn("transfer_file_progress", (data) => {
    fileProgressPercent.value = data.percent
    currentFile.value = data.filename
    updateManifestProgress(data.filename, data.percent)
    
    if (data.percent === 100) {
      completedFiles.add(data.filename)
      // addLog(`Completed: ${data.filename}`, 'success') // Optional, might be spammy
    }
  })
  
  EventsOn("transfer_global_progress", (data) => {
    globalSent.value = data.sent; globalTotal.value = data.total; globalProgressPercent.value = data.percent
  })
  
  EventsOn("transfer_complete", (msg) => {
    const transferDurationSeconds = (Date.now() - transferStartTime) / 1000
    const avgSpeed = transferDurationSeconds > 0 ? formatSize(globalTotal.value / transferDurationSeconds) : '0 B'
    addLog(`✓ Transfer complete! Average speed: ${avgSpeed}/s`, 'success')
    isSending.value = false; isReceiving.value = false; isConnecting.value = false
    globalProgressPercent.value = 100; fileProgressPercent.value = 100; transferComplete.value = true
    currentFile.value = msg; transferSpeed.value = 0
    if (speedInterval) clearInterval(speedInterval)
  })

  EventsOn("log", (msg) => {
    // Enhance network logs with more context
    if (msg.includes('Bootstrapping')) {
      addLog('→ Connecting to DHT bootstrap nodes...', 'info')
    } else if (msg.includes('Network ready')) {
      addLog('✓ P2P network ready, advertising transfer code', 'success')
    } else if (msg.includes('Finding peer')) {
      addLog('→ Searching DHT for sender...', 'info')
    } else if (msg.includes('Searching for sender')) {
      // Add retry count if present
      const match = msg.match(/\((\d+)s\)/)
      if (match) {
        addLog(`⟳ Still searching... (${match[1]}s elapsed)`, 'info')
      }
    } else if (msg.includes('Peer connected')) {
      addLog('✓ Peer found and connected!', 'success')
    } else if (msg.includes('Connecting...')) {
      addLog('→ Establishing secure connection...', 'info')
    } else if (msg.includes('Retrying')) {
      addLog(msg.replace('Retrying transfer', '⟳ Retrying transfer'), 'info')
    } else {
      addLog(msg, 'info')
    }
  })
  
  loadHistory()
  
  EventsOn("wails:file-drop", (files) => {
    if (files && files.length > 0) {
      addLog(`File dropped: ${files[0]}`, 'info')
      sendPath.value = files[0]
      mode.value = 'send'
    }
  })

  // Auto-update events
  EventsOn("update_available", (data) => {
    updateAvailable.value = data
    updateDismissed.value = false
    addLog(`Update available: v${data.version}`, 'info')
  })

  EventsOn("update_download_progress", (data) => {
    updateProgress.value = data.percent
  })

  EventsOn("update_ready", (data) => {
    addLog(`Update v${data.version} ready. Restarting...`, 'success')
    // App will restart automatically
  })

  EventsOn("update_error", (data) => {
    addLog(`Update error: ${data.error}`, 'error')
    updateDownloading.value = false
  })

  // Load app version
  GetVersion().then(v => appVersion.value = v)
})

async function pickFile() { 
  const path = await SelectFile(); 
  if (path) { sendPath.value = path; addLog(`Selected file: ${path}`, 'info') } 
}
async function pickFolder() { 
  const path = await SelectFolder(); 
  if (path) { sendPath.value = path; addLog(`Selected folder: ${path}`, 'info') }
}
async function pickDest() {
  const path = await SelectSaveDirectory();
  if (path) { destPath.value = path; addLog(`Selected destination: ${path}`, 'info') }
}

function downloadUpdate() {
  updateDownloading.value = true
  updateProgress.value = 0
  DownloadAndInstallUpdate(updateAvailable.value.version)
    .catch(err => {
      addLog(`Failed to install update: ${err}`, 'error')
      updateDownloading.value = false
    })
}

async function startSend() {
  if (!sendPath.value) return
  resetState(); isConnecting.value = true
  addLog(`Initiating send for: ${sendPath.value}`, 'system')
  try { sendCode.value = await StartSender(sendPath.value, settings.compress, !settings.autoHash, settings.cacheManifest) } 
  catch (e) { errorMsg.value = e; isConnecting.value = false; addLog(`Send failed: ${e}`, 'error') }
}

async function startRecv() {
  if (!recvCode.value || !destPath.value) return
  resetState(); isConnecting.value = true
  addLog(`Initiating receive with code: ${recvCode.value}`, 'system')
  try { await StartReceiver(recvCode.value, destPath.value, fastResume.value) } 
  catch (e) { errorMsg.value = e; isConnecting.value = false; addLog(`Receive failed: ${e}`, 'error') }
}

async function cancelTransfer() {
  addLog('Cancelling transfer...', 'error')
  await CancelTransfer()
  resetState()
}

function formatCode(e) {
  let val = e.target.value.replace(/[^0-9]/g, '')
  if (val.length > 3) val = val.slice(0, 3) + '-' + val.slice(3, 6)
  recvCode.value = val
}

async function copyCode() {
  if (sendCode.value) {
    await CopyToClipboard(sendCode.value)
    codeCopied.value = true
    addLog('Code copied to clipboard', 'info')
    setTimeout(() => codeCopied.value = false, 2000)
  }
}

function handleDragOver(e) {
  e.preventDefault()
  isDragging.value = true
}
function handleDragLeave(e) {
  e.preventDefault()
  if (e.target === e.currentTarget) {
    isDragging.value = false
  }
}
async function handleDrop(e) {
  e.preventDefault()
  isDragging.value = false
}

async function loadHistory() {
  try { const h = await GetTransferHistory(); if (h) transferHistory.value = h } catch(e) {}
}

async function resendFromHistory(record) {
  if (!record.fullPath) return

  // Check if file/folder still exists
  try {
    sendPath.value = record.fullPath
    mode.value = 'send'
    transferComplete.value = false
    addLog(`Preparing to resend: ${record.path}`, 'info')

    // Auto-start the transfer
    await startSend()
  } catch(e) {
    errorMsg.value = `Could not access path: ${record.fullPath}`
    addLog(`Failed to resend: ${e}`, 'error')
  }
}

const IconSend = `<svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z" stroke-linecap="round" stroke-linejoin="round"/></svg>`
const IconRecv = `<svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3" stroke-linecap="round" stroke-linejoin="round"/></svg>`
const IconHistory = `<svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14" stroke-linecap="round" stroke-linejoin="round"/></svg>`
const IconSettings = `<svg class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.1a2 2 0 0 1-1-1.72v-.51a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>`
const IconCopy = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>`

// Console panel resize functionality
function startResize(e) {
  isResizing.value = true
  const startY = e.clientY
  const startHeight = consolePanelHeight.value

  function onMouseMove(e) {
    if (!isResizing.value) return
    const delta = startY - e.clientY
    const newHeight = Math.max(100, Math.min(600, startHeight + delta))
    consolePanelHeight.value = newHeight
  }

  function onMouseUp() {
    isResizing.value = false
    document.removeEventListener('mousemove', onMouseMove)
    document.removeEventListener('mouseup', onMouseUp)
  }

  document.addEventListener('mousemove', onMouseMove)
  document.addEventListener('mouseup', onMouseUp)
}

</script>

<template>
  <div class="app-layout" @dragover="handleDragOver" @dragleave="handleDragLeave" @drop="handleDrop">
    <!-- Drag overlay -->
    <div v-if="isDragging" class="drag-overlay">
      <div class="drag-content">
        <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
          <polyline points="17 8 12 3 7 8"></polyline>
          <line x1="12" y1="3" x2="12" y2="15"></line>
        </svg>
        <div style="margin-top: 16px; font-size: 18px; font-weight: 600;">Drop to Send</div>
        <div style="margin-top: 4px; font-size: 14px; color: var(--text-secondary);">Release to select this file or folder</div>
      </div>
    </div>

    <!-- Sidebar -->
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-text">2c1f</div>
      </div>
      <nav>
        <div class="nav-item" :class="{active: mode === 'send'}" @click="mode = 'send'; transferComplete = false; addLog('Navigated to Send', 'info')">
          <span v-html="IconSend" class="nav-icon"></span> Send
        </div>
        <div class="nav-item" :class="{active: mode === 'receive'}" @click="mode = 'receive'; transferComplete = false; addLog('Navigated to Receive', 'info')">
          <span v-html="IconRecv" class="nav-icon"></span> Receive
        </div>
        <div class="nav-item" :class="{active: mode === 'history'}" @click="mode = 'history'; transferComplete = false; addLog('Navigated to History', 'info')">
          <span v-html="IconHistory" class="nav-icon"></span> History
        </div>
        <div class="nav-item" :class="{active: mode === 'settings'}" @click="mode = 'settings'; transferComplete = false; addLog('Navigated to Settings', 'info')">
          <span v-html="IconSettings" class="nav-icon"></span> Settings
        </div>
      </nav>
      <div class="sidebar-footer">
        {{ isSending ? 'Sending...' : isReceiving ? 'Receiving...' : 'Ready' }}
      </div>
    </aside>

    <!-- Main Content -->
    <main class="main-content">
      <div class="title-bar">
        <div class="window-controls">
          <div class="win-btn" @click="WindowMinimise">
             <svg class="win-icon" viewBox="0 0 10 1"><path d="M0 0h10v1H0z"/></svg>
          </div>
          <div class="win-btn" @click="WindowToggleMaximise">
             <svg class="win-icon" viewBox="0 0 10 10"><path d="M1 1h8v8H1z" fill="none" stroke="currentColor"/></svg>
          </div>
          <div class="win-btn close" @click="Quit">
             <svg class="win-icon" viewBox="0 0 10 10"><path d="M1 1l8 8M9 1L1 9" stroke="currentColor" stroke-width="1.2"/></svg>
          </div>
        </div>
      </div>

      <div class="content-area">
        <div class="page-header">
           <h1 class="page-title">{{ mode === 'send' ? 'Send Files' : mode === 'receive' ? 'Receive Files' : mode === 'history' ? 'History' : 'Settings' }}</h1>
           <div class="page-subtitle">{{ mode === 'send' ? 'Create a secure P2P transfer.' : mode === 'receive' ? 'Connect to a peer to download.' : mode === 'history' ? 'Recent file transfers.' : 'Configure application defaults.' }}</div>
        </div>

        <div v-if="errorMsg" class="card" style="margin-bottom: 24px; border-color: rgba(239,68,68,0.3); background: rgba(239,68,68,0.05); color: #ef4444;">
           {{ errorMsg }}
        </div>

        <!-- SEND MODE -->
        <div v-if="mode === 'send'">
           <div v-if="!isSending && !isConnecting && !transferComplete" class="card">
              <div class="input-group">
                 <label class="label">Select File or Folder</label>
                 <div class="input-row">
                    <input type="text" class="text-input" v-model="sendPath" placeholder="No file selected" readonly>
                    <button class="btn btn-secondary" @click="pickFile">File</button>
                    <button class="btn btn-secondary" @click="pickFolder">Folder</button>
                 </div>
              </div>
              <button class="btn btn-primary" @click="startSend" :disabled="!sendPath">
                 Create Transfer
              </button>
           </div>

           <div v-if="isConnecting && !isSending" class="card" style="align-items: center; text-align: center; padding: 48px;">
              <div v-if="!sendCode">
                 <div class="spinner" style="margin-bottom: 16px;"></div>
                 <div style="font-weight: 600; margin-bottom: 8px;">{{ senderStatus }}</div>

                 <!-- Detailed loading phases -->
                 <div v-if="loadingPhase === 'hashing'" style="color: var(--text-secondary); font-size: 13px;">
                    <div>Calculating file checksums...</div>
                    <div v-if="hashingFile" style="margin-top: 4px; font-family: monospace; font-size: 11px;">{{ hashingFile }}</div>
                 </div>
                 <div v-else-if="loadingPhase === 'p2p'" style="color: var(--text-secondary); font-size: 13px;">
                    Initializing P2P network node...
                 </div>
                 <div v-else-if="loadingPhase === 'init'" style="color: var(--text-secondary); font-size: 13px;">
                    Preparing transfer files...
                 </div>
                 <div v-else style="color: var(--text-secondary); font-size: 13px;">
                    Setting up transfer...
                 </div>
              </div>
              <div v-else class="code-display">
                 <div style="color: var(--text-secondary); font-size: 13px;">Share this code with receiver</div>
                 <div class="code-value">{{ sendCode }}</div>
                 <button class="btn btn-secondary" @click="copyCode">
                    <span v-html="IconCopy"></span> {{ codeCopied ? 'Copied!' : 'Copy Code' }}
                 </button>
                 <div style="margin-top: 16px; color: var(--text-secondary); font-size: 12px;">
                    Waiting for connection...
                 </div>
              </div>
              <div style="margin-top: 24px;">
                 <button class="btn btn-danger" @click="cancelTransfer">Cancel Transfer</button>
              </div>
           </div>
        </div>

        <!-- RECEIVE MODE -->
        <div v-if="mode === 'receive'">
           <div v-if="!isReceiving && !isConnecting && !transferComplete" class="card">
              <div class="input-group">
                 <label class="label">Connection Code</label>
                 <input type="text" class="text-input" :value="recvCode" @input="formatCode" placeholder="000-000" maxlength="7" style="font-family: monospace; font-size: 16px; letter-spacing: 1px;">
              </div>
              <div class="input-group">
                 <label class="label">Save Destination</label>
                 <div class="input-row">
                    <input type="text" class="text-input" v-model="destPath" placeholder="Select folder..." readonly>
                    <button class="btn btn-secondary" @click="pickDest">Browse</button>
                 </div>
              </div>
              <div class="checkbox-row">
                 <span>Fast Resume</span>
                 <input type="checkbox" v-model="fastResume" style="width: 16px; height: 16px;">
              </div>
              <div style="margin-top: 16px;">
                 <button class="btn btn-primary" @click="startRecv" :disabled="!recvCode || !destPath">Connect & Download</button>
              </div>
           </div>
           
           <div v-if="isConnecting && !isReceiving" class="card" style="align-items: center; text-align: center; padding: 48px;">
              <div class="spinner" style="margin-bottom: 16px;"></div>
              <div style="font-weight: 600; margin-bottom: 8px;">Connecting to Sender</div>
              <div style="color: var(--text-secondary); font-size: 13px;">
                 Searching for peer on DHT network...
              </div>
              <div style="margin-top: 8px; color: var(--text-secondary); font-size: 12px; font-family: monospace;">
                 Code: {{ recvCode }}
              </div>
              <div style="margin-top: 24px;">
                 <button class="btn btn-danger" @click="cancelTransfer">Cancel Transfer</button>
              </div>
           </div>
        </div>

        <!-- PROGRESS (Shared) -->
        <div v-if="isSending || isReceiving" class="card">
           <!-- Transfer name header -->
           <div style="margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--border-color);">
              <div style="font-size: 12px; color: var(--text-secondary); margin-bottom: 4px;">{{ isSending ? 'Sending' : 'Receiving' }}</div>
              <div style="font-size: 18px; font-weight: 700; color: var(--text-primary);">{{ transferName }}</div>
           </div>

           <div class="progress-container">
              <div class="progress-info">
                 <div style="font-weight: 600; font-size: 15px;">Overall Progress</div>
                 <div style="display: flex; align-items: baseline; gap: 8px;">
                    <div style="font-weight: 600; font-size: 18px;">{{ Math.round(globalProgressPercent) }}%</div>
                    <div style="color: var(--text-secondary); font-size: 13px;">{{ formattedTransferred }} / {{ formattedTotal }}</div>
                 </div>
              </div>
              <div class="progress-track">
                 <div class="progress-fill" :style="{width: globalProgressPercent + '%'}"></div>
              </div>

              <!-- Current File Progress -->
              <div v-if="currentFile" style="margin-top: 20px; padding-top: 20px; border-top: 1px solid var(--border-color);">
                 <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
                    <div style="font-size: 13px; color: var(--text-secondary);">Current File</div>
                    <div style="font-size: 13px; font-weight: 600; color: var(--text-primary);">{{ Math.round(fileProgressPercent) }}%</div>
                 </div>
                 <div style="font-size: 14px; font-weight: 500; margin-bottom: 8px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ currentFile }}</div>
                 <div class="progress-track" style="height: 4px;">
                    <div class="progress-fill" :style="{width: fileProgressPercent + '%', backgroundColor: 'var(--success)'}"></div>
                 </div>
              </div>

              <div class="stat-grid">
                 <div class="stat-item">
                    <div class="stat-label">Transfer Speed</div>
                    <div class="stat-value">{{ formattedSpeed }}</div>
                 </div>
                 <div class="stat-item">
                    <div class="stat-label">Time Remaining</div>
                    <div class="stat-value">{{ formattedEta }}</div>
                 </div>
                 <div class="stat-item">
                    <div class="stat-label">Files Completed</div>
                    <div class="stat-value">{{ completedFiles.size }} / {{ manifestFiles.length }}</div>
                 </div>
                 <div class="stat-item">
                    <div class="stat-label">Status</div>
                    <div class="stat-value" style="font-size: 14px; color: var(--success);">{{ isSending ? 'Sending' : 'Receiving' }}</div>
                 </div>
              </div>
           </div>
           
           <!-- File List with Progress -->
           <div class="file-list" v-if="manifestFiles.length > 0">
              <div v-for="(file, i) in manifestFiles" :key="i" class="file-item" :style="{opacity: file.progress >= 100 ? 0.6 : 1}">
                 <div class="file-row-main">
                    <div class="file-icon" :style="{color: file.progress >= 100 ? 'var(--success)' : 'var(--text-secondary)'}">
                       <svg v-if="file.progress >= 100" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                          <polyline points="20 6 9 17 4 12"></polyline>
                       </svg>
                       <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                          <path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path>
                          <polyline points="13 2 13 9 20 9"></polyline>
                       </svg>
                    </div>
                    <div class="file-name">{{ file.path }}</div>
                    <div style="font-size: 11px; color: var(--text-secondary); margin-right: 8px;">
                       {{ formatSize(file.size) }}
                    </div>
                    <div style="font-size: 12px; font-weight: 500; width: 45px; text-align: right;" :style="{color: file.progress >= 100 ? 'var(--success)' : 'var(--text-primary)'}">
                       {{ file.progress ? Math.round(file.progress) + '%' : '0%' }}
                    </div>
                 </div>
                 <div class="file-progress-mini">
                    <div class="file-progress-fill" :style="{width: (file.progress || 0) + '%'}"></div>
                 </div>
              </div>
           </div>

           <div style="margin-top: 24px; text-align: right;">
              <button class="btn btn-danger" @click="cancelTransfer">Cancel</button>
           </div>
        </div>

        <!-- COMPLETE -->
        <div v-if="transferComplete" class="card" style="text-align: center; padding: 56px 48px;">
           <div style="color: var(--success); margin-bottom: 20px; display: inline-block;">
              <div style="width: 64px; height: 64px; border-radius: 50%; background: rgba(16, 185, 129, 0.1); display: flex; align-items: center; justify-content: center; margin: 0 auto;">
                 <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
                    <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>
                 </svg>
              </div>
           </div>
           <h2 style="font-size: 22px; font-weight: 700; margin-bottom: 8px; color: var(--text-primary);">Transfer Complete!</h2>
           <p style="color: var(--text-secondary); margin-bottom: 32px; font-size: 14px;">{{ transferName }} has been successfully transferred</p>
           <button class="btn btn-primary" @click="resetState" style="min-width: 140px;">New Transfer</button>
        </div>

        <!-- SETTINGS -->
        <div v-if="mode === 'settings'" class="card">
           <div class="checkbox-row">
              <div>
                 <div style="font-weight: 500;">Auto Hash</div>
                 <div style="font-size: 12px; color: var(--text-secondary);">Verify integrity before transfer</div>
              </div>
              <input type="checkbox" v-model="settings.autoHash" @change="updateSettings">
           </div>
           <div class="checkbox-row">
              <div>
                 <div style="font-weight: 500;">Compression</div>
                 <div style="font-size: 12px; color: var(--text-secondary);">Use gzip (High CPU)</div>
              </div>
              <input type="checkbox" v-model="settings.compress" @change="updateSettings">
           </div>
        </div>

        <!-- HISTORY -->
        <div v-if="mode === 'history'" class="card" style="padding: 0; overflow: hidden; max-height: 600px; display: flex; flex-direction: column;">
           <div v-if="transferHistory.length === 0" style="padding: 32px; text-align: center; color: var(--text-secondary);">
              No transfer history found.
           </div>
           <div v-else style="overflow-y: auto; flex: 1;">
              <div v-for="(record, i) in transferHistory" :key="i" class="history-item">
                 <div class="direction-icon" :style="{color: record.direction === 'send' ? '#60a5fa' : '#34d399'}">
                    <span v-html="record.direction === 'send' ? IconSend : IconRecv" style="width: 16px; height: 16px;"></span>
                 </div>
                 <div style="flex: 1; min-width: 0;">
                    <div style="font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ record.path }}</div>
                    <div style="font-size: 12px; color: var(--text-secondary);">{{ formatSize(record.size) }} • {{ new Date(record.timestamp).toLocaleDateString() }}</div>
                 </div>
                 <div style="display: flex; align-items: center; gap: 12px;">
                    <div :style="{color: record.status === 'complete' ? 'var(--success)' : 'var(--danger)'}" style="font-size: 12px; font-weight: 500; text-transform: capitalize; min-width: 70px; text-align: right;">
                       {{ record.status }}
                    </div>
                    <button v-if="record.direction === 'send' && record.fullPath"
                            @click="resendFromHistory(record)"
                            class="btn btn-secondary"
                            style="padding: 6px 12px; font-size: 12px; min-width: 80px;">
                       {{ record.status === 'complete' ? 'Resend' : 'Retry' }}
                    </button>
                 </div>
              </div>
           </div>
        </div>

      </div>
    </main>

    <!-- Update Notification (bottom-right corner) -->
    <div v-if="updateAvailable && !updateDismissed" class="update-notification">
      <div class="update-header">
        <span class="update-title">Update Available: v{{ updateAvailable.version }}</span>
        <button @click="updateDismissed = true" class="dismiss-btn" title="Dismiss">×</button>
      </div>
      <div class="update-body">
        <p>A new version of 2c1f is available.</p>
        <button
          v-if="!updateDownloading"
          @click="downloadUpdate"
          class="btn btn-primary btn-sm">
          Download Now
        </button>
        <div v-else class="update-progress">
          <div class="progress-bar-thin">
            <div class="progress-fill" :style="{width: updateProgress + '%'}"></div>
          </div>
          <span class="progress-text">{{ updateProgress.toFixed(1) }}%</span>
        </div>
      </div>
    </div>

    <!-- Network Activity Panel (Bottom Collapsible) -->
    <div class="console-panel" :class="{collapsed: consoleCollapsed, resizing: isResizing}">
      <div class="console-panel-header" @click="consoleCollapsed = !consoleCollapsed">
        <div style="display: flex; align-items: center; gap: 8px;">
          <svg class="collapse-icon" :class="{rotated: !consoleCollapsed}" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="18 15 12 9 6 15"></polyline>
          </svg>
          <span style="font-size: 12px; font-weight: 600; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.5px;">Network Activity</span>
        </div>
        <div v-if="consoleLogs.length > 0" style="font-size: 11px; color: var(--text-secondary);">{{ consoleLogs.length }} event{{ consoleLogs.length !== 1 ? 's' : '' }}</div>
      </div>
      <div class="console-panel-content" :style="{height: consoleCollapsed ? '0px' : consolePanelHeight + 'px'}">
        <div class="resize-handle" @mousedown.prevent="startResize" v-show="!consoleCollapsed">
          <div class="resize-handle-bar"></div>
        </div>
        <div class="console-panel-logs" ref="logContainer" v-show="!consoleCollapsed">
          <div v-if="consoleLogs.length === 0" style="color: #666; text-align: center; padding: 20px;">
            No network activity yet...
          </div>
          <div v-for="(log, i) in consoleLogs" :key="i" class="log-entry">
            <div class="log-time">[{{ log.time }}]</div>
            <div class="log-msg" :class="log.type">{{ log.msg }}</div>
          </div>
        </div>
      </div>
    </div>

  </div>
</template>

<style scoped>
/* Update Notification */
.update-notification {
  position: fixed;
  bottom: 24px;
  right: 24px;
  width: 320px;
  background: rgba(24, 24, 27, 0.95);
  border: 1px solid rgba(59, 130, 246, 0.5);
  border-radius: 8px;
  padding: 16px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.4);
  z-index: 1000;
  animation: slideInRight 0.3s ease-out;
  backdrop-filter: blur(10px);
}

@keyframes slideInRight {
  from {
    transform: translateX(400px);
    opacity: 0;
  }
  to {
    transform: translateX(0);
    opacity: 1;
  }
}

.update-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.update-title {
  font-weight: 600;
  color: #3b82f6;
  font-size: 14px;
}

.dismiss-btn {
  background: none;
  border: none;
  color: #71717a;
  font-size: 24px;
  line-height: 1;
  cursor: pointer;
  padding: 0;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: all 0.2s;
}

.dismiss-btn:hover {
  background: rgba(255, 255, 255, 0.1);
  color: #a1a1aa;
}

.update-body p {
  margin: 0 0 12px 0;
  color: #a1a1aa;
  font-size: 13px;
}

.btn-sm {
  padding: 6px 12px;
  font-size: 13px;
}

.update-progress {
  display: flex;
  align-items: center;
  gap: 10px;
}

.progress-bar-thin {
  flex: 1;
  height: 6px;
  background: rgba(255, 255, 255, 0.1);
  border-radius: 3px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #3b82f6, #60a5fa);
  transition: width 0.3s ease-out;
}

.progress-text {
  font-size: 12px;
  color: #71717a;
  font-family: 'Consolas', 'Monaco', monospace;
  min-width: 45px;
  text-align: right;
}

/* Console Panel (Bottom Collapsible) */
.console-panel {
  position: fixed;
  bottom: 0;
  left: 240px; /* Sidebar width - FIXED */
  right: 0;
  background: var(--bg-app);
  border-top: 1px solid var(--border-color);
  z-index: 100;
}

.console-panel.collapsed {
  /* Header stays visible */
}

.console-panel.resizing {
  user-select: none;
}

.resize-handle {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 6px;
  cursor: ns-resize;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10;
}

.resize-handle:hover .resize-handle-bar {
  background: var(--accent-color);
  opacity: 0.3;
}

.resize-handle-bar {
  width: 60px;
  height: 3px;
  background: var(--border-color);
  border-radius: 2px;
  opacity: 0.5;
  transition: all 0.15s;
}

.console-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 20px;
  cursor: pointer;
  user-select: none;
  background: var(--bg-sidebar);
  transition: background 0.15s;
  height: 36px;
}

.console-panel-header:hover {
  background: var(--bg-hover);
}

.console-panel-content {
  overflow: hidden;
  background: var(--bg-app);
  position: relative;
}

.console-panel:not(.resizing) .console-panel-content {
  transition: height 0.2s ease-in-out;
}

.console-panel-logs {
  height: calc(100% - 6px); /* Account for resize handle */
  overflow-y: auto;
  padding: 12px 20px;
  padding-top: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  scroll-behavior: smooth;
}

.console-panel-logs::-webkit-scrollbar {
  width: 8px;
}

.console-panel-logs::-webkit-scrollbar-track {
  background: transparent;
}

.console-panel-logs::-webkit-scrollbar-thumb {
  background: var(--border-color);
  border-radius: 4px;
}

.console-panel-logs::-webkit-scrollbar-thumb:hover {
  background: var(--bg-hover);
}

.collapse-icon {
  transition: transform 0.15s ease-in-out;
}

.collapse-icon.rotated {
  transform: rotate(180deg);
}
</style>