<script setup lang="ts">
import { computed } from 'vue'
import { APIError } from '../api/client'
const props = defineProps<{ error: Error | null; success?: string }>()
const fields = computed(() => (props.error instanceof APIError ? props.error.validations : []))
</script>

<template>
  <div v-if="error" class="notice error" role="alert">
    <p>{{ error.message }}</p>
    <ul v-if="fields.length">
      <li v-for="field in fields" :key="field.field">{{ field.detail }}</li>
    </ul>
  </div>
  <p v-else-if="success" class="notice success" role="status">{{ success }}</p>
</template>
