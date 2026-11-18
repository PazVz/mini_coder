import { createApp } from 'vue'
import App from './App.vue'
import { createAppRouter } from './router'
import { captureEmailProof } from './emailProof'
import './style.css'

captureEmailProof()
createApp(App).use(createAppRouter()).mount('#app')
