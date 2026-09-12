<script lang="ts">
  import { onMount } from 'svelte';

  export let title: string;
  export let description = '';
  export let dismissible = true;
  export let onClose: () => void;

  let panel: HTMLDivElement;

  onMount(() => {
    const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    panel?.focus();
    return () => previouslyFocused?.focus();
  });

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && dismissible) {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key !== 'Tab' || !panel) return;
    const focusable = [
      ...panel.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'
      )
    ].filter((element) => !element.hasAttribute('hidden'));
    if (focusable.length === 0) {
      event.preventDefault();
      panel.focus();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div
  class="modal-backdrop"
  role="presentation"
  onclick={(event) => event.target === event.currentTarget && dismissible && onClose()}
>
  <div
    class="modal-panel"
    role="dialog"
    aria-modal="true"
    aria-labelledby="modal-title"
    tabindex="-1"
    bind:this={panel}
  >
    <div class="modal-heading">
      <div>
        <h2 id="modal-title">{title}</h2>
        {#if description}<p>{description}</p>{/if}
      </div>
      {#if dismissible}
        <button class="icon-button" type="button" aria-label="بستن" onclick={onClose}>
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg>
        </button>
      {/if}
    </div>
    <slot />
  </div>
</div>
