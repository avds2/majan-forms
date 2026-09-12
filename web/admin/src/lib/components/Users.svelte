<script lang="ts">
  import { onMount } from 'svelte';
  import { api, APIError } from '../api';
  import { formatDateTime, toPersianDigits } from '../persian-date';
  import type { Role, User } from '../types';
  import Modal from './Modal.svelte';

  export let user: User;

  const roleLabels: Record<Role, string> = {
    admin: 'مدیر',
    designer: 'طراح فرم',
    viewer: 'مشاهده‌گر'
  };

  let users: User[] = [];
  let loading = true;
  let error = '';
  let createOpen = false;
  let submitting = false;
  let savingID = '';
  let form = { email: '', username: '', display_name: '', password: '', role: 'viewer' as Role };

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      users = await api.users();
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      loading = false;
    }
  }

  function openCreate() {
    form = { email: '', username: '', display_name: '', password: '', role: 'viewer' };
    error = '';
    createOpen = true;
  }

  async function createUser() {
    submitting = true;
    error = '';
    try {
      const created = await api.createUser(form);
      users = [...users, created];
      createOpen = false;
    } catch (caught) {
      error = messageOf(caught);
    } finally {
      submitting = false;
    }
  }

  async function update(target: User, changes: Partial<Pick<User, 'display_name' | 'role' | 'active'>>) {
    const original = target;
    const next = { ...target, ...changes };
    users = users.map((candidate) => (candidate.id === target.id ? next : candidate));
    savingID = target.id;
    error = '';
    try {
      const saved = await api.updateUser(target.id, {
        display_name: next.display_name,
        role: next.role,
        active: next.active
      });
      users = users.map((candidate) => (candidate.id === saved.id ? saved : candidate));
    } catch (caught) {
      users = users.map((candidate) => (candidate.id === original.id ? original : candidate));
      error = messageOf(caught);
    } finally {
      savingID = '';
    }
  }

  function messageOf(caught: unknown): string {
    return caught instanceof APIError ? caught.message : 'ارتباط با سرور برقرار نشد.';
  }
</script>

<div class="page-container users-page">
  <header class="page-header">
    <div>
      <p class="eyebrow">مدیریت سازمان</p>
      <h1>کاربران</h1>
      <p>نقش‌ها در همهٔ پروژه‌ها اعمال می‌شوند.</p>
    </div>
    <button class="button primary" type="button" onclick={openCreate}>
      <svg viewBox="0 0 24 24" aria-hidden="true"
        ><path d="M15 19a6 6 0 0 0-12 0m6-8a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm10-2v6m3-3h-6" /></svg
      >
      کاربر تازه
    </button>
  </header>

  <section class="role-guide" aria-label="راهنمای نقش‌ها">
    <div>
      <span class="role-icon admin">م</span>
      <p><strong>مدیر</strong><small>کاربران، تنظیمات و همهٔ پروژه‌ها</small></p>
    </div>
    <div>
      <span class="role-icon designer">ط</span>
      <p><strong>طراح فرم</strong><small>ساخت و ویرایش پروژه و پیش‌نویس فرم</small></p>
    </div>
    <div>
      <span class="role-icon viewer">ن</span>
      <p><strong>مشاهده‌گر</strong><small>دسترسی فقط‌خواندنی به پروژه‌ها و فرم‌ها</small></p>
    </div>
  </section>

  {#if error}<div class="alert error" role="alert">{error}</div>{/if}

  <section class="table-card">
    <div class="table-heading">
      <div>
        <h2>اعضای سازمان</h2>
        <span>{toPersianDigits(users.length)} کاربر</span>
      </div>
    </div>
    {#if loading}
      <div class="table-loading">
        <div class="loader"></div>
        <span>در حال دریافت کاربران…</span>
      </div>
    {:else}
      <div class="responsive-table">
        <table>
          <thead><tr><th>کاربر</th><th>نقش</th><th>آخرین ورود</th><th>وضعیت</th></tr></thead>
          <tbody>
            {#each users as member}
              <tr class:muted={!member.active}>
                <td data-label="کاربر">
                  <div class="person-cell">
                    <div class="avatar">{member.display_name.slice(0, 1)}</div>
                    <div>
                      <strong
                        >{member.display_name}{#if member.id === user.id}<span class="you">شما</span>{/if}</strong
                      ><span dir="ltr">{member.email}</span><small>@{member.username}</small>
                    </div>
                  </div>
                </td>
                <td data-label="نقش">
                  <select
                    aria-label={`نقش ${member.display_name}`}
                    value={member.role}
                    disabled={member.id === user.id || savingID === member.id}
                    onchange={(event) => update(member, { role: event.currentTarget.value as Role })}
                  >
                    {#each Object.entries(roleLabels) as [value, label]}<option {value}>{label}</option>{/each}
                  </select>
                </td>
                <td data-label="آخرین ورود"><span class="date-value">{formatDateTime(member.last_login_at)}</span></td>
                <td data-label="وضعیت">
                  <label class="switch">
                    <input
                      type="checkbox"
                      checked={member.active}
                      disabled={member.id === user.id || savingID === member.id}
                      onchange={(event) => update(member, { active: event.currentTarget.checked })}
                    />
                    <span aria-hidden="true"></span><em>{member.active ? 'فعال' : 'غیرفعال'}</em>
                  </label>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
</div>

{#if createOpen}
  <Modal
    title="کاربر تازه"
    description="رمز اولیه را از یک مسیر امن در اختیار کاربر بگذارید."
    onClose={() => (createOpen = false)}
  >
    <form
      class="modal-form two-column-form"
      onsubmit={(event) => {
        event.preventDefault();
        createUser();
      }}
    >
      {#if error}<div class="alert error full" role="alert">{error}</div>{/if}
      <div class="field-group full">
        <label for="new-name">نام نمایشی</label><input id="new-name" bind:value={form.display_name} required />
      </div>
      <div class="field-group">
        <label for="new-email">ایمیل</label><input
          id="new-email"
          dir="ltr"
          type="email"
          autocomplete="off"
          bind:value={form.email}
          required
        />
      </div>
      <div class="field-group">
        <label for="new-username">نام کاربری</label><input
          id="new-username"
          dir="ltr"
          pattern={'[^\\s]{3,64}'}
          autocomplete="off"
          bind:value={form.username}
          required
        />
      </div>
      <div class="field-group">
        <label for="new-role">نقش</label><select id="new-role" bind:value={form.role}
          >{#each Object.entries(roleLabels) as [value, label]}<option {value}>{label}</option>{/each}</select
        >
      </div>
      <div class="field-group">
        <label for="new-password">رمز اولیه</label><input
          id="new-password"
          type="password"
          minlength="12"
          autocomplete="new-password"
          bind:value={form.password}
          required
        /><small>دست‌کم ۱۲ نویسه</small>
      </div>
      <div class="modal-actions full">
        <button class="button secondary" type="button" onclick={() => (createOpen = false)}>انصراف</button><button
          class="button primary"
          type="submit"
          disabled={submitting}>{submitting ? 'در حال ساخت…' : 'ساخت کاربر'}</button
        >
      </div>
    </form>
  </Modal>
{/if}
