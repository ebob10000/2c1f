<script setup>
import { computed } from 'vue'

const props = defineProps({
  node: Object,
  currentFile: String,
  completedFiles: Set
})

const isOpen = computed(() => props.node.isOpen)

const toggle = () => {
  if (props.node.type === 'dir') {
    props.node.isOpen = !props.node.isOpen
  }
}

const status = computed(() => {
  if (props.node.type === 'dir') return ''
  if (props.completedFiles.has(props.node.fullPath)) return 'done'
  if (props.currentFile === props.node.fullPath) return 'loading'
  return 'pending'
})

// Icons
const IconFolder = `<svg width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>`
const IconFile = `<svg width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>`
const IconCheck = `<svg width="14" height="14" stroke="#10b981" stroke-width="3" viewBox="0 0 24 24" fill="none"><polyline points="20 6 9 17 4 12"></polyline></svg>`

</script>

<template>
  <div class="tree-node">
    <div class="node-row" @click="toggle" :class="{ 'is-dir': node.type === 'dir' }">
       <span class="status-icon">
          <span v-if="status === 'done'" v-html="IconCheck"></span>
          <div v-else-if="status === 'loading'" class="mini-spinner"></div>
          <span v-else class="dot"></span>
       </span>
       
       <span class="type-icon" v-html="node.type === 'dir' ? IconFolder : IconFile"></span>
       <span class="node-name">{{ node.name }}</span>
       <span v-if="node.type === 'dir'" class="arrow" :class="{ open: node.isOpen }">▶</span>
    </div>

    <div v-if="node.type === 'dir' && node.isOpen" class="children">
       <TreeNode v-for="child in node.childrenArray" :key="child.name" 
                 :node="child" :currentFile="currentFile" :completedFiles="completedFiles" />
    </div>
  </div>
</template>

<style>
.tree-node {
  margin-left: 8px;
}
.node-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px;
  border-radius: 4px;
  cursor: default;
}
.node-row.is-dir {
  cursor: pointer;
  font-weight: 600;
  color: var(--text-primary);
}
.node-row.is-dir:hover {
  background: rgba(255,255,255,0.05);
}
.node-name {
  color: var(--text-secondary);
}
.is-dir .node-name { color: var(--text-primary); }

.status-icon {
  width: 14px; display: flex; align-items: center; justify-content: center;
}
.dot { width: 4px; height: 4px; background: #333; border-radius: 50%; }

.children {
  margin-left: 8px;
  padding-left: 8px;
  border-left: 1px solid var(--border-color);
}
.arrow {
  font-size: 8px; color: var(--text-muted); margin-left: auto; transition: transform 0.2s;
}
.arrow.open { transform: rotate(90deg); }
</style>
