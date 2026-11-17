(() => {
  let token = new URLSearchParams(location.hash.slice(1)).get("token") || "";
  history.replaceState(null, "", location.pathname);
  const status = document.getElementById("status");

  function bindForm(id, path, payload, success) {
    const form = document.getElementById(id);
    form.hidden = false;
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const button = form.querySelector("button");
      button.disabled = true;
      status.textContent = "正在处理…";
      try {
        const response = await fetch(path, {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload(new FormData(form))),
        });
        const data = await response.json();
        if (!response.ok) {
          status.textContent = data.validations?.map((field) => field.detail).join("；") || data.message || "操作失败，请稍后重试。";
          if (data.code === "unauthenticated") {
            status.textContent = "请先登录对应账号，再点击确认邮箱。";
          }
          return;
        }
        status.textContent = success(data);
        form.reset();
        if (id !== "request-form") {
          token = "";
          form.hidden = true;
        }
      } catch {
        status.textContent = "网络请求失败，请稍后重试。";
      } finally {
        button.disabled = false;
      }
    });
  }

  if (document.body.dataset.flow === "register") {
    if (token) {
      document.getElementById("intro").textContent = "设置用户名和密码，完成邮箱验证与注册。";
      bindForm("register-form", "/api/v1/auth/register", (form) => ({
        token, username: form.get("username"), password: form.get("password"),
      }), () => "注册成功。请返回 mini_coder 登录。");
    } else {
      bindForm("request-form", "/api/v1/auth/register/request", (form) => ({
        email: form.get("email"),
      }), (data) => data.message);
    }
  } else if (token) {
    bindForm("confirm-form", "/api/v1/auth/email/confirm", () => ({ token }), (data) => `主邮箱 ${data.email} 已验证。`);
  } else {
    status.textContent = "链接缺少验证信息，请从邮件重新打开或重新申请。";
  }
})();
