// Provider console progressive enhancement. Events are delegated from the
// document because HTMX replaces the page body after connection mutations.
(() => {
  "use strict";

  const modelCache = new Map();
  let activeCredential = "";
  let activeProvider = "";
  let chatController = null;
  let oauthProvider = "";
  let oauthFlowID = "";

  const byID = (id) => document.getElementById(id);
  const setResult = (element, message, state = "") => {
    if (!element) return;
    element.textContent = message;
    const baseClass = element.id === "chat-output" ? "chat-output" : "provider-result";
    element.className = `${baseClass}${state ? ` ${state}` : ""}`;
  };
  const responseError = async (response) => {
    try {
      const body = await response.json();
      return body?.error?.message || body?.message || `Request failed (HTTP ${response.status})`;
    } catch (_) {
      return `Request failed (HTTP ${response.status})`;
    }
  };
  const fetchJSON = async (url, options = {}) => {
    const response = await fetch(url, {
      credentials: "same-origin",
      ...options,
      headers: { Accept: "application/json", ...(options.headers || {}) },
    });
    if (!response.ok) throw new Error(await responseError(response));
    return response.json();
  };
  const showDialog = (dialog) => {
    if (!dialog) return;
    if (typeof dialog.showModal === "function") dialog.showModal();
    else dialog.setAttribute("open", "");
  };

  async function discoverModels(credentialID, providerName, targetDialog) {
    activeCredential = credentialID;
    activeProvider = providerName;
    const dialog = byID(targetDialog);
    const result = byID(targetDialog === "models-dialog" ? "models-dialog-result" : "chat-output");
    if (targetDialog === "models-dialog") {
      byID("models-dialog-provider").textContent = `${providerName} · loading available models…`;
      byID("models-dialog-list").replaceChildren();
      byID("models-dialog-list").setAttribute("aria-busy", "true");
      setResult(result, "");
    } else {
      byID("chat-dialog-provider").textContent = `${providerName} · direct credential test (may incur provider cost)`;
      byID("chat-model").replaceChildren(new Option("Loading models…", ""));
      setResult(result, "Loading available models…", "loading");
    }
    showDialog(dialog);
    try {
      let models = modelCache.get(credentialID);
      if (!models) {
        const payload = await fetchJSON(`/admin/credentials/${encodeURIComponent(credentialID)}/models`);
        models = Array.isArray(payload.data) ? payload.data : [];
        modelCache.set(credentialID, models);
      }
      if (targetDialog === "models-dialog") renderModelPicker(models, providerName);
      else renderChatModels(models);
    } catch (error) {
      if (targetDialog === "models-dialog") {
        byID("models-dialog-list").setAttribute("aria-busy", "false");
        setResult(result, error.message, "error");
      } else {
        byID("chat-model").replaceChildren(new Option("No models available", ""));
        setResult(result, error.message, "error");
      }
    }
  }

  function renderModelPicker(models, providerName) {
    const list = byID("models-dialog-list");
    list.replaceChildren();
    list.setAttribute("aria-busy", "false");
    byID("models-dialog-provider").textContent = `${providerName} · ${models.length} model${models.length === 1 ? "" : "s"} discovered`;
    if (!models.length) {
      const empty = document.createElement("p");
      empty.className = "muted empty-state";
      empty.textContent = "This provider returned no models for the credential.";
      list.append(empty);
      return;
    }
    const fragment = document.createDocumentFragment();
    for (const model of models) {
      const label = document.createElement("label");
      label.className = "model-option";
      const checkbox = document.createElement("input");
      checkbox.type = "checkbox";
      checkbox.value = model.id;
      checkbox.name = "provider-model";
      const copy = document.createElement("span");
      const title = document.createElement("strong");
      title.textContent = model.public_id || model.id;
      const meta = document.createElement("small");
      const details = [model.id];
      if (model.owned_by) details.push(`owner: ${model.owned_by}`);
      if (model.context_length) details.push(`${Number(model.context_length).toLocaleString()} context`);
      meta.textContent = details.join(" · ");
      copy.append(title, meta);
      label.append(checkbox, copy);
      fragment.append(label);
    }
    list.append(fragment);
    updateSelectAllLabel();
  }

  function renderChatModels(models) {
    const select = byID("chat-model");
    select.replaceChildren();
    for (const model of models) select.add(new Option(model.public_id || model.id, model.id));
    if (!models.length) {
      select.add(new Option("No models available", ""));
    } else {
      const markedDefault = models.findIndex((model) => model.default === true);
      select.selectedIndex = markedDefault >= 0 ? markedDefault : 0;
    }
    setResult(byID("chat-output"), models.length ? "Ready. Send a prompt to start the stream." : "No models were returned by this provider.", models.length ? "" : "error");
  }

  function updateSelectAllLabel() {
    const button = byID("models-select-all");
    if (!button) return;
    const boxes = [...document.querySelectorAll('#models-dialog-list input[type="checkbox"]')];
    button.textContent = boxes.length && boxes.every((box) => box.checked) ? "Clear selection" : "Select all";
  }

  async function testCredential(button) {
    const credentialID = button.dataset.credentialTest;
    const result = byID(`credential-result-${credentialID}`);
    button.disabled = true;
    setResult(result, "Testing provider health…", "loading");
    try {
      const payload = await fetchJSON(`/admin/credentials/${encodeURIComponent(credentialID)}/test`, { method: "POST" });
      const healthy = payload.ok === true;
      const latency = Number.isFinite(payload.latency_ms) ? ` in ${payload.latency_ms} ms` : "";
      const status = payload.status ? ` · HTTP ${payload.status}` : "";
      setResult(result, healthy ? `Healthy${latency}${status}` : `Health check failed${status}`, healthy ? "success" : "error");
      const dot = button.closest("[data-credential-account]")?.querySelector(".status-dot");
      if (dot) dot.classList.toggle("ok", healthy);
    } catch (error) {
      setResult(result, error.message, "error");
    } finally {
      button.disabled = false;
    }
  }

  async function importModels(button) {
    const selected = [...document.querySelectorAll('#models-dialog-list input[type="checkbox"]:checked')].map((box) => box.value);
    const result = byID("models-dialog-result");
    if (!selected.length) {
      setResult(result, "Select at least one model to import.", "error");
      return;
    }
    button.disabled = true;
    setResult(result, `Importing ${selected.length} model${selected.length === 1 ? "" : "s"}…`, "loading");
    try {
      const payload = await fetchJSON(`/admin/credentials/${encodeURIComponent(activeCredential)}/models/import`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ models: selected }),
      });
      const imported = Array.isArray(payload.imported) ? payload.imported.length : selected.length;
      setResult(result, `Imported ${imported} model${imported === 1 ? "" : "s"}.`, "success");
    } catch (error) {
      setResult(result, error.message, "error");
    } finally {
      button.disabled = false;
    }
  }

  function appendStreamPayload(output, data) {
    if (!data || data === "[DONE]") return data === "[DONE]";
    try {
      const payload = JSON.parse(data);
      const content = payload?.choices?.[0]?.delta?.content;
      if (typeof content === "string") output.textContent += content;
      if (payload?.error?.message) output.textContent += `\nError: ${payload.error.message}`;
    } catch (_) {
      output.textContent += data;
    }
    return false;
  }

  async function streamChat(button) {
    const model = byID("chat-model").value;
    const prompt = byID("chat-prompt").value.trim();
    const output = byID("chat-output");
    if (!model || !prompt) {
      setResult(output, "Choose a model and enter a prompt.", "error");
      return;
    }
    if (chatController) chatController.abort();
    chatController = new AbortController();
    button.disabled = true;
    byID("chat-stop").hidden = false;
    setResult(output, "", "streaming");
    try {
      const response = await fetch(`/admin/credentials/${encodeURIComponent(activeCredential)}/chat-tests`, {
        method: "POST",
        credentials: "same-origin",
        headers: { Accept: "text/event-stream", "Content-Type": "application/json" },
        body: JSON.stringify({ model, prompt }),
        signal: chatController.signal,
      });
      if (!response.ok) throw new Error(await responseError(response));
      if (!response.body) throw new Error("Streaming is not supported by this browser.");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let doneEvent = false;
      while (!doneEvent) {
        const chunk = await reader.read();
        buffer += decoder.decode(chunk.value || new Uint8Array(), { stream: !chunk.done });
        const events = buffer.split(/\r?\n\r?\n/);
        buffer = events.pop() || "";
        for (const event of events) {
          const data = event.split(/\r?\n/).filter((line) => line.startsWith("data:"))
            .map((line) => line.slice(5).trimStart()).join("\n");
          doneEvent = appendStreamPayload(output, data) || doneEvent;
        }
        if (chunk.done) break;
      }
      if (!output.textContent) output.textContent = "The stream completed without text output.";
      output.className = "chat-output success";
    } catch (error) {
      if (error.name === "AbortError") output.textContent += "\n[stopped]";
      else setResult(output, error.message, "error");
    } finally {
      chatController = null;
      button.disabled = false;
      byID("chat-stop").hidden = true;
    }
  }

  async function startOAuth(button) {
    oauthProvider = button.dataset.oauthStart;
    oauthFlowID = "";
    const dialog = byID("oauth-dialog");
    const link = byID("oauth-authorize-link");
    byID("oauth-dialog-title").textContent = `Connect ${button.textContent.replace(/^Connect\s+/, "")}`;
    byID("oauth-callback").value = "";
    link.hidden = true;
    link.removeAttribute("href");
    setResult(byID("oauth-dialog-result"), "Creating a secure authorization flow…", "loading");
    showDialog(dialog);
    button.disabled = true;
    try {
      const payload = await fetchJSON(`/admin/oauth/${encodeURIComponent(oauthProvider)}/start`, { method: "POST" });
      oauthFlowID = payload.flow_id || "";
      if (!oauthFlowID || !payload.authorize_url) throw new Error("OAuth start response was incomplete.");
      link.href = payload.authorize_url;
      link.hidden = false;
      setResult(byID("oauth-dialog-result"), payload.instructions || "Open the authorization page, then paste the callback here.", "success");
    } catch (error) {
      setResult(byID("oauth-dialog-result"), error.message, "error");
    } finally {
      button.disabled = false;
    }
  }

  async function completeOAuth(button) {
    const callback = byID("oauth-callback").value.trim();
    if (!oauthFlowID || !callback) {
      setResult(byID("oauth-dialog-result"), "Start the flow and paste the callback first.", "error");
      return;
    }
    button.disabled = true;
    setResult(byID("oauth-dialog-result"), "Exchanging and encrypting OAuth credentials…", "loading");
    try {
      const owner = byID("oauth-owner")?.value || "";
      await fetchJSON(`/admin/oauth/${encodeURIComponent(oauthProvider)}/complete`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ flow_id: oauthFlowID, callback, name: byID("oauth-name").value.trim(), owner_tenant_id: owner || null }),
      });
      oauthFlowID = "";
      setResult(byID("oauth-dialog-result"), "Connected. Refreshing providers…", "success");
      window.setTimeout(() => window.location.reload(), 450);
    } catch (error) {
      setResult(byID("oauth-dialog-result"), error.message, "error");
    } finally {
      button.disabled = false;
    }
  }

  document.addEventListener("click", (event) => {
    const button = event.target.closest("button");
    if (!button) return;
    if (button.dataset.credentialTest) testCredential(button);
    else if (button.dataset.oauthStart) startOAuth(button);
    else if (button.dataset.credentialModels) discoverModels(button.dataset.credentialModels, button.dataset.providerName, "models-dialog");
    else if (button.dataset.credentialChat) discoverModels(button.dataset.credentialChat, button.dataset.providerName, "chat-dialog");
    else if (button.id === "models-select-all") {
      const boxes = [...document.querySelectorAll('#models-dialog-list input[type="checkbox"]')];
      const select = !boxes.every((box) => box.checked);
      boxes.forEach((box) => { box.checked = select; });
      updateSelectAllLabel();
    } else if (button.id === "models-import") importModels(button);
    else if (button.id === "chat-send") streamChat(button);
    else if (button.id === "chat-stop" && chatController) chatController.abort();
    else if (button.id === "oauth-complete") completeOAuth(button);
  });
  document.addEventListener("change", (event) => {
    if (event.target.matches('#models-dialog-list input[type="checkbox"]')) updateSelectAllLabel();
  });
  document.addEventListener("close", (event) => {
    if (event.target.id === "chat-dialog" && chatController) chatController.abort();
  }, true);
})();
