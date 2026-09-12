document.addEventListener('click', (event) => {
  const trigger = event.target.closest('[data-print-report]');
  if (trigger) window.print();
});
