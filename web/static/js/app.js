const $ = (id) => document.getElementById(id);
const dropZone = $("drop-zone");
const fileInput = $("file-input");

if (dropZone) {
  dropZone.onclick = () => {
    if ($("idle-state").style.display !== "none") fileInput.click();
  };

  fileInput.onchange = () => {
    if (fileInput.files[0]) handleUpload(fileInput.files[0]);
  };

  ["dragenter", "dragover"].forEach((n) =>
    dropZone.addEventListener(n, (e) => {
      e.preventDefault();
      dropZone.classList.add("dragover");
    }),
  );

  ["dragleave", "drop"].forEach((n) =>
    dropZone.addEventListener(n, (e) => {
      e.preventDefault();
      dropZone.classList.remove("dragover");
    }),
  );

  dropZone.addEventListener("drop", (e) => {
    e.preventDefault();
    if (e.dataTransfer.files.length) handleUpload(e.dataTransfer.files[0]);
  });
}

async function handleUpload(file) {
  $("idle-state").style.display = "none";
  $("busy-state").style.display = "block";

  const uploadID = Math.random().toString(36).substring(2, 15);
  const chunkSize = 1024 * 1024 * 8;
  const total = Math.ceil(file.size / chunkSize);

  try {
    for (let i = 0; i < total; i++) {
      const fd = new FormData();
      fd.append("upload_id", uploadID);
      fd.append("index", i);
      fd.append("chunk", file.slice(i * chunkSize, (i + 1) * chunkSize));
      const res = await fetch("/upload/chunk", { method: "POST", body: fd });
      if (!res.ok) throw new Error();
      $("p-fill").style.width = ((i + 1) / total) * 100 + "%";
    }

    const finalFd = new FormData();
    finalFd.append("upload_id", uploadID);
    finalFd.append("filename", file.name);
    finalFd.append("total", total);

    const res = await fetch("/upload/finish", {
      method: "POST",
      body: finalFd,
      headers: { "X-Requested-With": "XMLHttpRequest" },
    });

    $("busy-state").style.display = "none";
    $("result-state").innerHTML = await res.text();
  } catch (e) {
    $("busy-state").style.display = "none";
    $("result-state").innerHTML = `<div class="error-text">Upload Failed</div><button class="reset-btn" onclick="resetUI()">Try again</button>`;
  }
}

function copyToClipboard(btn) {
  const input = $("share-url");
  input.select();
  const fullUrl = window.location.protocol + "//" + input.value;
  navigator.clipboard.writeText(fullUrl);
  btn.innerText = "Copied!";
  setTimeout(() => (btn.innerText = "Copy"), 2000);
}

function resetUI() {
  location.reload();
}
