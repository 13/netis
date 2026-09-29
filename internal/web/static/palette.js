(function () {
	// Command palette (Ctrl/⌘-K) and keyboard shortcuts. The palette's fixed
	// entries (pages, settings, actions) come from the server in
	// <template id="palette-static">, already filtered by role; devices and
	// subnets come from GET /api/search as the user types.
	var dlg = document.getElementById('palette');
	var help = document.getElementById('shortcuts');
	if (!dlg || typeof dlg.showModal !== 'function') { return; }
	var input = document.getElementById('palette-q');
	var list = document.getElementById('palette-list');
	var empty = dlg.querySelector('.palette-empty');
	var mac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

	// Show the controls that need this script.
	document.querySelectorAll('[data-palette-open], [data-shortcuts-open]').forEach(function (b) { b.hidden = false; });
	document.querySelectorAll('[data-mod-key]').forEach(function (k) { k.textContent = mac ? '⌘ K' : 'Ctrl K'; });

	var fixed = [];
	var tpl = document.getElementById('palette-static');
	if (tpl) {
		tpl.content.querySelectorAll('li').forEach(function (li) {
			fixed.push({
				group: li.dataset.group, label: li.textContent, icon: li.dataset.icon,
				href: li.dataset.href || '', action: li.dataset.action || '',
				keys: (li.dataset.keys || '').toLowerCase(), shortcut: li.dataset.shortcut || ''
			});
		});
	}

	var items = [];   // what the list shows now, in order
	var active = -1;  // index into items of the highlighted option
	var remote = { q: null, devices: [], subnets: [] };
	var timer = null, seq = 0, opener = null;

	function icon(name) {
		var ns = 'http://www.w3.org/2000/svg';
		var svg = document.createElementNS(ns, 'svg');
		svg.setAttribute('class', 'icon');
		svg.setAttribute('aria-hidden', 'true');
		var use = document.createElementNS(ns, 'use');
		use.setAttribute('href', '/static/icons.svg#' + name);
		svg.appendChild(use);
		return svg;
	}

	// Every word of the query must appear in the label or the keywords.
	function matches(e, words) {
		var hay = (e.label + ' ' + e.keys).toLowerCase();
		for (var i = 0; i < words.length; i++) { if (hay.indexOf(words[i]) < 0) { return false; } }
		return true;
	}

	function render() {
		var q = input.value.trim().toLowerCase();
		var words = q ? q.split(/\s+/) : [];
		items = [];
		if (q) {
			remote.devices.forEach(function (d) {
				items.push({ group: 'Devices', label: d.name, icon: d.icon || 'circle-help', href: '/devices/' + d.id,
					detail: [d.ip, d.mac].filter(Boolean).join(' · '), mono: true, online: d.online });
			});
			remote.subnets.forEach(function (s) {
				items.push({ group: 'Subnets', label: s.name, icon: 'network', href: '/subnets/' + s.id, detail: s.cidr, mono: true });
			});
		}
		fixed.forEach(function (e) {
			if (!q ? e.group !== 'Settings' : matches(e, words)) { items.push(e); }
		});
		list.textContent = '';
		var group = null;
		items.forEach(function (it, i) {
			if (it.group !== group) {
				group = it.group;
				var h = document.createElement('li');
				h.className = 'palette-group';
				h.setAttribute('role', 'presentation');
				h.textContent = group;
				list.appendChild(h);
			}
			var li = document.createElement('li');
			li.id = 'palette-opt-' + i;
			li.className = 'palette-opt';
			li.setAttribute('role', 'option');
			li.setAttribute('aria-selected', 'false');
			li.dataset.index = i;
			li.appendChild(icon(it.icon));
			var lbl = document.createElement('span');
			lbl.className = 'palette-label';
			lbl.textContent = it.label;
			li.appendChild(lbl);
			if (it.detail) {
				var d = document.createElement('span');
				d.className = 'palette-detail' + (it.mono ? ' mono' : '');
				d.textContent = it.detail;
				li.appendChild(d);
			}
			if (it.shortcut) {
				var k = document.createElement('span');
				k.className = 'palette-keys';
				it.shortcut.split(' ').forEach(function (c) {
					var kbd = document.createElement('kbd');
					kbd.textContent = c;
					k.appendChild(kbd);
				});
				li.appendChild(k);
			}
			list.appendChild(li);
		});
		empty.hidden = items.length > 0;
		select(items.length ? 0 : -1);
	}

	function select(i) {
		var prev = list.querySelector('[aria-selected="true"]');
		if (prev) { prev.setAttribute('aria-selected', 'false'); }
		active = i;
		if (i < 0) { input.removeAttribute('aria-activedescendant'); return; }
		var el = document.getElementById('palette-opt-' + i);
		el.setAttribute('aria-selected', 'true');
		input.setAttribute('aria-activedescendant', el.id);
		el.scrollIntoView({ block: 'nearest' });
	}

	// Ask the server for devices and subnets, debounced. A late answer to an
	// older query is dropped.
	function fetchRemote() {
		var q = input.value.trim();
		clearTimeout(timer);
		if (!q) { remote = { q: '', devices: [], subnets: [] }; render(); return; }
		timer = setTimeout(function () {
			var mine = ++seq;
			fetch('/api/search?q=' + encodeURIComponent(q), { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
				.then(function (r) { return r.ok ? r.json() : { devices: [], subnets: [] }; })
				.then(function (j) {
					if (mine !== seq) { return; }
					remote = { q: q, devices: j.devices || [], subnets: j.subnets || [] };
					render();
				})
				.catch(function () {});
		}, 150);
	}

	function setTheme(t) {
		if (window.netisTheme) { window.netisTheme(t); }
	}

	function run(it) {
		if (!it) { return; }
		close();
		if (it.href) { location.href = it.href; return; }
		switch (it.action) {
			case 'new-device': newDevice(); break;
			case 'scan-all':
				if (window.htmx) { htmx.ajax('POST', '/scan', { target: '#toasts', swap: 'beforeend' }); }
				break;
			case 'shortcuts': showHelp(); break;
			case 'theme-system': setTheme('system'); break;
			case 'theme-light': setTheme('light'); break;
			case 'theme-dark': setTheme('dark'); break;
		}
	}

	function newDevice() {
		if (window.htmx) { htmx.ajax('GET', '/devices/new', { target: '#modal' }); } else { location.href = '/devices/new'; }
	}

	function open() {
		if (dlg.open) { return; }
		if (help.open) { help.close(); }
		opener = document.activeElement;
		input.value = '';
		remote = { q: '', devices: [], subnets: [] };
		render();
		dlg.showModal();
		input.focus();
	}

	function close() {
		if (dlg.open) { dlg.close(); }
	}

	function showHelp() {
		if (dlg.open) { dlg.close(); }
		if (!help.open) { opener = document.activeElement; help.showModal(); }
	}

	dlg.addEventListener('close', function () {
		if (opener && opener.isConnected && opener.focus) { opener.focus(); }
	});
	// A click on the backdrop reports the dialog itself as the target.
	[dlg, help].forEach(function (d) {
		d.addEventListener('click', function (e) {
			if (e.target === d || e.target.closest('[data-dialog-close]')) { d.close(); }
		});
	});

	input.addEventListener('input', function () { render(); fetchRemote(); });
	input.addEventListener('keydown', function (e) {
		if (e.key === 'ArrowDown') { e.preventDefault(); if (items.length) { select((active + 1) % items.length); } }
		else if (e.key === 'ArrowUp') { e.preventDefault(); if (items.length) { select((active - 1 + items.length) % items.length); } }
		else if (e.key === 'Home' && items.length) { e.preventDefault(); select(0); }
		else if (e.key === 'End' && items.length) { e.preventDefault(); select(items.length - 1); }
		else if (e.key === 'Enter') { e.preventDefault(); run(items[active]); }
	});
	list.addEventListener('mousemove', function (e) {
		var li = e.target.closest('.palette-opt');
		if (li && +li.dataset.index !== active) { select(+li.dataset.index); }
	});
	list.addEventListener('click', function (e) {
		var li = e.target.closest('.palette-opt');
		if (li) { run(items[+li.dataset.index]); }
	});

	document.addEventListener('click', function (e) {
		if (e.target.closest('[data-palette-open]')) { open(); }
		else if (e.target.closest('[data-shortcuts-open]')) {
			var d = e.target.closest('details');
			if (d) { d.open = false; }
			showHelp();
		}
	});

	// Keyboard shortcuts. None fire while typing in a field or while a dialog
	// is open (Escape and the dialog's own keys work there), except Ctrl/⌘-K,
	// which opens the palette from anywhere.
	function typing(el) {
		if (!el) { return false; }
		var t = el.tagName;
		return t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT' || el.isContentEditable;
	}
	var pendingG = 0;
	var goTo = { h: '/', d: '/devices', s: '/subnets', e: '/events' };
	document.addEventListener('keydown', function (e) {
		if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			if (dlg.open) { close(); } else { open(); }
			return;
		}
		if (e.ctrlKey || e.metaKey || e.altKey || typing(e.target) || document.querySelector('dialog[open]')) { return; }
		if (pendingG && Date.now() - pendingG < 1200 && goTo[e.key]) {
			e.preventDefault();
			pendingG = 0;
			location.href = goTo[e.key];
			return;
		}
		pendingG = 0;
		switch (e.key) {
			case 'g': pendingG = Date.now(); break;
			case '/':
				e.preventDefault();
				var s = document.querySelector('main input[type="search"]');
				if (s) { s.focus(); s.select(); } else { open(); }
				break;
			case '?': e.preventDefault(); showHelp(); break;
			case 'n':
				if (fixed.some(function (f) { return f.action === 'new-device'; })) {
					e.preventDefault();
					newDevice();
				}
				break;
		}
	});
})();
