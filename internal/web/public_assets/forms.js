(() => {
  if (new URLSearchParams(window.location.search).has("_measure")) {
    const root = document.documentElement;
    const recordNavigation = () => {
      const navigation = performance.getEntriesByType("navigation")[0];
      if (!navigation) return;
      root.dataset.ttfbMs = Math.round(navigation.responseStart).toString();
      root.dataset.domContentLoadedMs = Math.round(navigation.domContentLoadedEventEnd).toString();
      root.dataset.loadMs = Math.round(navigation.loadEventEnd).toString();
    };
    recordNavigation();
    window.addEventListener("load", () => requestAnimationFrame(recordNavigation), { once: true });
    try {
      new PerformanceObserver((entries) => {
        const latest = entries.getEntries().at(-1);
        if (latest) root.dataset.lcpMs = Math.round(latest.startTime).toString();
      }).observe({ type: "largest-contentful-paint", buffered: true });
    } catch {
      root.dataset.lcpMs = "unsupported";
    }
  }

  for (const form of document.querySelectorAll("[data-final-submit]")) {
    form.addEventListener("submit", () => {
      const button = form.querySelector('button[type="submit"]');
      if (button) {
        button.disabled = true;
        button.textContent = "در حال ثبت…";
      }
    });
  }

  for (const input of document.querySelectorAll(".file-input")) {
    const status = input.parentElement?.querySelector("[data-file-status]");
    input.addEventListener("change", () => {
      if (!status) return;
      const names = Array.from(input.files ?? [], (file) => file.name);
      status.textContent = names.length ? names.join("، ") : "هنوز فایلی انتخاب نشده است";
    });
  }

  for (const box of document.querySelectorAll("[data-signature]")) {
    const canvas = box.querySelector("canvas");
    const input = box.querySelector('input[type="hidden"]');
    const clear = box.querySelector("[data-clear-signature]");
    if (!canvas || !input) continue;
    const context = canvas.getContext("2d");
    if (!context) continue;
    context.lineWidth = 3;
    context.lineCap = "round";
    context.strokeStyle = "#15263a";
    let drawing = false;
    const point = (event) => {
      const rect = canvas.getBoundingClientRect();
      return [
        (event.clientX - rect.left) * (canvas.width / rect.width),
        (event.clientY - rect.top) * (canvas.height / rect.height),
      ];
    };
    canvas.addEventListener("pointerdown", (event) => {
      drawing = true;
      canvas.setPointerCapture(event.pointerId);
      context.beginPath();
      context.moveTo(...point(event));
    });
    canvas.addEventListener("pointermove", (event) => {
      if (!drawing) return;
      context.lineTo(...point(event));
      context.stroke();
    });
    const finish = () => {
      if (!drawing) return;
      drawing = false;
      input.value = canvas.toDataURL("image/png");
    };
    canvas.addEventListener("pointerup", finish);
    canvas.addEventListener("pointercancel", finish);
    clear?.addEventListener("click", () => {
      context.clearRect(0, 0, canvas.width, canvas.height);
      input.value = "";
    });
  }
})();
