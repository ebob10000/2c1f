<script setup>
import { ref, watch } from 'vue'
import TreeNode from './TreeNode.vue'

const props = defineProps({
  files: Array,
  currentFile: String,
  completedFiles: Set // Set of paths
})

const tree = ref([])

watch(() => props.files, (newFiles) => {
  if (!newFiles || newFiles.length === 0) {
    tree.value = []
    return
  }
  
  // Build tree
  const root = { name: 'root', children: {}, type: 'dir' }
  newFiles.forEach(file => {
    const parts = file.path.split('/')
    let current = root
    parts.forEach((part, index) => {
      if (index === parts.length - 1) {
        current.children[part] = {
           name: part,
           type: 'file',
           fullPath: file.path,
           size: file.size
        }
      } else {
        if (!current.children[part]) {
          current.children[part] = {
            name: part,
            type: 'dir',
            children: {},
            isOpen: true // Default open
          }
        }
        current = current.children[part]
      }
    })
  })

  // Helper to convert object map to sorted array
  const toArray = (node) => {
    return Object.values(node.children).sort((a, b) => {
      if (a.type !== b.type) return a.type === 'dir' ? -1 : 1
      return a.name.localeCompare(b.name)
    }).map(child => {
      if (child.type === 'dir') {
        child.childrenArray = toArray(child)
      }
      return child
    })
  }

  tree.value = toArray(root)
}, { immediate: true })

</script>

<template>
  <div class="file-tree-inner">
    <div v-for="node in tree" :key="node.name">
       <TreeNode :node="node" :currentFile="currentFile" :completedFiles="completedFiles" />
    </div>
  </div>
</template>

<style>
.file-tree-inner {
  text-align: left;
  font-family: var(--font-mono);
  font-size: 13px;
  padding: 12px;
}
.mini-spinner { width: 10px; height: 10px; border: 2px solid var(--accent-color); border-top-color: transparent; border-radius: 50%; animation: spin 0.6s linear infinite; display: inline-block; }
</style>