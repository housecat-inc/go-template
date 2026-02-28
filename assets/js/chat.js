(function() {
	const messages = document.getElementById('messages');
	const form = document.getElementById('chat-form');
	const input = document.getElementById('chat-input');
	if (!form || !input) return;

	let chatID = new URLSearchParams(window.location.search).get('id') || '';
	let streaming = false;

	let history = [];
	if (messages) {
		messages.querySelectorAll(':scope > div.flex').forEach(function(el) {
			const isUser = el.classList.contains('justify-end');
			const bubble = el.querySelector('div');
			if (bubble) {
				history.push({role: isUser ? 'user' : 'assistant', content: bubble.textContent});
			}
		});
		messages.scrollTop = messages.scrollHeight;
	}

	document.querySelectorAll('.chat-template').forEach(function(btn) {
		btn.addEventListener('click', function() {
			input.value = 'Help me with: ' + btn.dataset.prompt;
			input.focus();
		});
	});

	function getOrCreateMessages() {
		let el = document.getElementById('messages');
		if (el) return el;
		const container = form.closest('.flex.h-full');
		if (!container) return null;
		const emptyState = container.querySelector('.flex-1.flex.items-center');
		if (emptyState) emptyState.remove();
		const wrapper = document.createElement('div');
		wrapper.className = 'flex-1 flex flex-col min-w-0';
		const msgArea = document.createElement('div');
		msgArea.id = 'messages';
		msgArea.className = 'flex-1 overflow-y-auto p-4 space-y-4';
		const inputArea = document.createElement('div');
		inputArea.className = 'border-t p-4';
		const formParent = form.parentElement;
		inputArea.appendChild(form);
		if (formParent && formParent.children.length === 0) formParent.remove();
		wrapper.appendChild(msgArea);
		wrapper.appendChild(inputArea);
		container.appendChild(wrapper);
		return msgArea;
	}

	function addBubble(role, text) {
		const msgEl = getOrCreateMessages();
		if (!msgEl) return null;
		const isUser = role === 'user';
		const wrap = document.createElement('div');
		wrap.className = 'flex ' + (isUser ? 'justify-end' : 'justify-start');
		const bubble = document.createElement('div');
		bubble.className = 'max-w-[80%] rounded-lg px-4 py-2 text-sm whitespace-pre-wrap ' +
			(isUser ? 'bg-primary text-primary-foreground' : 'bg-muted text-foreground');
		bubble.textContent = text;
		wrap.appendChild(bubble);
		msgEl.appendChild(wrap);
		msgEl.scrollTop = msgEl.scrollHeight;
		return bubble;
	}

	form.addEventListener('submit', async function(e) {
		e.preventDefault();
		const text = input.value.trim();
		if (!text || streaming) return;
		streaming = true;
		input.value = '';
		form.querySelector('button[type="submit"]').disabled = true;

		addBubble('user', text);
		history.push({role: 'user', content: text});

		const bubble = addBubble('assistant', '');
		let fullText = '';

		try {
			const resp = await fetch('/api/chat', {
				method: 'POST',
				headers: {'Content-Type': 'application/json'},
				body: JSON.stringify({chat_id: chatID, messages: history})
			});
			if (!resp.ok) {
				if (bubble) bubble.textContent = 'Error: ' + resp.statusText;
				return;
			}
			const reader = resp.body.getReader();
			const decoder = new TextDecoder();
			let buf = '';

			while (true) {
				const {done, value} = await reader.read();
				if (done) break;
				buf += decoder.decode(value, {stream: true});
				const lines = buf.split('\n');
				buf = lines.pop();
				for (const line of lines) {
					if (!line.startsWith('data: ')) continue;
					const data = line.slice(6);
					if (data === '[DONE]') continue;
					try {
						const parsed = JSON.parse(data);
						if (parsed.chat_id && !chatID) {
							chatID = parsed.chat_id;
							window.history.replaceState(null, '', '/chat?id=' + chatID);
						}
						if (parsed.text && bubble) {
							fullText += parsed.text;
							bubble.textContent = fullText;
							const msgEl = document.getElementById('messages');
							if (msgEl) msgEl.scrollTop = msgEl.scrollHeight;
						}
					} catch(ex) {}
				}
			}
			history.push({role: 'assistant', content: fullText});
		} catch(err) {
			if (bubble) bubble.textContent = 'Error: ' + err.message;
		} finally {
			streaming = false;
			form.querySelector('button[type="submit"]').disabled = false;
			input.focus();
		}
	});
})();
