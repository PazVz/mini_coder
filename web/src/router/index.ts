import { createRouter, createWebHistory } from 'vue-router'
import { discardEmailProofExcept, rememberEmailProof } from '../emailProof'
import SetupView from '../views/SetupView.vue'
import RegisterView from '../views/RegisterView.vue'
import LoginView from '../views/LoginView.vue'
import ConfirmEmailView from '../views/ConfirmEmailView.vue'
import AccountView from '../views/AccountView.vue'
import NotFoundView from '../views/NotFoundView.vue'

export function createAppRouter() {
  const router = createRouter({
    history: createWebHistory(),
    routes: [
      { path: '/', redirect: '/account' },
      { path: '/auth/setup', component: SetupView, meta: { title: '初始化' } },
      { path: '/auth/register', component: RegisterView, meta: { title: '注册' } },
      { path: '/auth/login', component: LoginView, meta: { title: '登录' } },
      { path: '/auth/email/confirm', component: ConfirmEmailView, meta: { title: '确认邮箱' } },
      { path: '/account', component: AccountView, meta: { title: '我的账号' } },
      { path: '/:pathMatch(.*)*', component: NotFoundView, meta: { title: '页面不存在' } },
    ],
    scrollBehavior: () => ({ top: 0 }),
  })
  router.beforeEach((to) => {
    discardEmailProofExcept(to.path)
    if (rememberEmailProof(to.path, to.hash)) {
      return { path: to.path, query: to.query, hash: '', replace: true }
    }
  })
  return router
}
