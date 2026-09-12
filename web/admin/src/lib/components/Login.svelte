<script lang="ts">
  import { api, APIError } from '../api';
  import type { User } from '../types';

  export let onLoggedIn: (user: User) => void;

  let identifier = '';
  let password = '';
  let error = '';
  let submitting = false;

  async function submit() {
    error = '';
    submitting = true;
    try {
      onLoggedIn(await api.login(identifier, password));
      password = '';
    } catch (caught) {
      error = caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
    } finally {
      submitting = false;
    }
  }
</script>

<main class="login-page">
  <section class="login-brand" aria-label="معرفی سامانه">
    <div class="brand-mark large" aria-hidden="true"><span></span><span></span><span></span></div>
    <div>
      <p class="eyebrow">سامانهٔ فرم‌های سازمانی</p>
      <h1>فرم خوب، مسیر روشن.</h1>
      <p>ساخت و مدیریت پیش‌نویس فرم‌های فارسی در یک فضای امن و یکپارچه.</p>
    </div>
    <div class="brand-lines" aria-hidden="true"><i></i><i></i><i></i><i></i></div>
  </section>
  <section class="login-card-wrap">
    <form
      class="login-card"
      onsubmit={(event) => {
        event.preventDefault();
        submit();
      }}
    >
      <div class="mobile-brand">
        <div class="brand-mark" aria-hidden="true"><span></span><span></span><span></span></div>
        <strong>فرم‌ها</strong>
      </div>
      <div class="form-heading">
        <p class="eyebrow">ورود به پنل</p>
        <h2>خوش آمدید</h2>
        <p>با حساب سازمانی خود وارد شوید.</p>
      </div>
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
      <div class="field-group">
        <label for="identifier">ایمیل یا نام کاربری</label>
        <input id="identifier" name="identifier" autocomplete="username" bind:value={identifier} required />
      </div>
      <div class="field-group">
        <label for="password">رمز عبور</label>
        <input
          id="password"
          name="password"
          type="password"
          autocomplete="current-password"
          bind:value={password}
          required
        />
      </div>
      <button class="button primary wide" type="submit" disabled={submitting}>
        {submitting ? 'در حال ورود…' : 'ورود به پنل'}
        {#if !submitting}<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 12h14m-6-6 6 6-6 6" /></svg>{/if}
      </button>
      <p class="login-help">برای بازیابی دسترسی با مدیر سامانه تماس بگیرید.</p>
    </form>
  </section>
</main>
