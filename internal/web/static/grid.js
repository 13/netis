// The subnet page's patch panel. The server draws every port as a submit
// button in one form, which works without JavaScript (a click loads the page
// with that port open). This file makes it a keyboard grid with a single tab
// stop, opens a port's details in the side panel in place, and keeps the
// selection and focus across the live refreshes that replace the ports. All
// of it hangs off a few listeners on the document, never one per port: a /22
// has 1,024 of them.
(function () {
	var panel = document.getElementById('cell-panel');
	if (!panel) { return; }
	var subnet = panel.getAttribute('data-subnet');
	var empty = document.getElementById('cp-empty');
	var cp = panel.querySelector('.cp[data-ip]');
	var selected = cp ? cp.getAttribute('data-ip') : null;

	function form() { return document.querySelector('form.patch'); }
	function ports() { var f = form(); return f ? f.querySelectorAll('.port') : []; }
	function portFor(ip) {
		var f = form();
		return f && ip ? f.querySelector('.port[value="' + ip + '"]') : null;
	}

	// One port is in the tab order: the open one, else the one last focused,
	// else the first.
	function roving(ip) {
		var all = ports();
		if (!all.length) { return; }
		var keep = portFor(ip) || all[0];
		for (var i = 0; i < all.length; i++) { all[i].tabIndex = -1; }
		keep.tabIndex = 0;
	}

	function markSelected(ip) {
		var f = form();
		if (!f) { return; }
		var old = f.querySelectorAll('[role="gridcell"][aria-selected="true"]');
		for (var i = 0; i < old.length; i++) { old[i].removeAttribute('aria-selected'); }
		var p = portFor(ip);
		if (p) { p.parentNode.setAttribute('aria-selected', 'true'); }
	}

	function setURL(ip) {
		try {
			var u = new URL(window.location.href);
			if (ip) { u.searchParams.set('ip', ip); } else { u.searchParams.delete('ip'); }
			history.replaceState(null, '', u.pathname + u.search + u.hash);
		} catch (e) {}
	}

	function loadPanel(ip, focus) {
		htmx.ajax('GET', '/subnets/' + subnet + '/cell?ip=' + encodeURIComponent(ip), {
			target: '#cell-panel', swap: 'innerHTML'
		}).then(function () {
			if (focus) {
				var h = document.getElementById('cp-title');
				if (h) { h.focus(); }
			}
		});
	}

	function open(ip, focusPanel) {
		selected = ip;
		markSelected(ip);
		roving(ip);
		panel.setAttribute('data-open', '');
		setURL(ip);
		loadPanel(ip, focusPanel);
	}

	function close() {
		var ip = selected;
		selected = null;
		markSelected(null);
		markRange(null);
		panel.removeAttribute('data-open');
		panel.innerHTML = empty ? empty.innerHTML : '';
		setURL(null);
		var p = portFor(ip);
		if (p) { p.focus(); }
	}

	// Ports per visual line: 16, or 8 when a very narrow screen wraps each
	// row in two. Counted from the layout so the arrows follow what is seen.
	function perLine(all) {
		var top = all[0].offsetTop, n = 1;
		while (n < all.length && n < 16 && all[n].offsetTop === top) { n++; }
		return n;
	}

	document.addEventListener('keydown', function (e) {
		var p = e.target;
		if (!p.classList || !p.classList.contains('port') || !p.closest('form.patch')) {
			if (e.key === 'Escape' && selected && panel.contains(e.target) && !document.querySelector('#modal dialog[open]')) {
				e.preventDefault();
				close();
			}
			return;
		}
		var all = Array.prototype.slice.call(ports());
		var i = all.indexOf(p), n = all.length, cols = perLine(all), j = i;
		switch (e.key) {
		case 'ArrowRight': j = Math.min(i + 1, n - 1); break;
		case 'ArrowLeft': j = Math.max(i - 1, 0); break;
		case 'ArrowDown': j = i + cols < n ? i + cols : i; break;
		case 'ArrowUp': j = i - cols >= 0 ? i - cols : i; break;
		case 'Home': j = e.ctrlKey ? 0 : i - (i % cols); break;
		case 'End': j = e.ctrlKey ? n - 1 : Math.min(i - (i % cols) + cols - 1, n - 1); break;
		case 'PageDown': j = Math.min(i + cols * 4, n - 1); break;
		case 'PageUp': j = Math.max(i - cols * 4, 0); break;
		case 'Escape':
			hideCard();
			if (selected) { e.preventDefault(); close(); }
			return;
		case 'c': case 'C':
			if (e.ctrlKey || e.metaKey || e.altKey) { return; }
			e.preventDefault();
			if (window.netisCopy) { window.netisCopy(p.value); }
			return;
		default: return;
		}
		e.preventDefault();
		if (j !== i) {
			p.tabIndex = -1;
			all[j].tabIndex = 0;
			all[j].focus();
		}
	});

	// A port click opens its details in place instead of loading the page.
	// A keyboard press (Enter, Space) reports no pointer detail; then the
	// address heading takes focus so the details are read out.
	document.addEventListener('click', function (e) {
		var p = e.target.closest && e.target.closest('form.patch .port');
		if (p) {
			e.preventDefault();
			open(p.value, e.detail === 0);
			return;
		}
		var c = e.target.closest && e.target.closest('[data-cell-close]');
		if (c && panel.contains(c)) {
			e.preventDefault();
			close();
			return;
		}
		var rc = e.target.closest && e.target.closest('[data-range]');
		if (rc && portFor(rc.getAttribute('data-range'))) {
			e.preventDefault();
			showRange(rc.getAttribute('data-range'), rc.getAttribute('data-range-end'));
			return;
		}
		var fo = e.target.closest && e.target.closest('[data-free-only]');
		if (fo) {
			setFreeOnly(fo.getAttribute('aria-pressed') !== 'true');
			return;
		}
		var nf = e.target.closest && e.target.closest('[data-next-free]');
		if (nf) {
			var ip = nf.getAttribute('data-next-free');
			var port = portFor(ip);
			if (!port) { return; }
			e.preventDefault();
			open(ip, false);
			port.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
			port.focus({ preventScroll: true });
			port.classList.remove('flash');
			void port.offsetWidth;
			port.classList.add('flash');
		}
	});

	// ---- free ranges: a chip marks its run of ports and opens the first ----
	var range = null;
	function markRange(r) {
		range = r;
		var f = form();
		if (!f) { return; }
		var old = f.querySelectorAll('.port.in-range');
		for (var i = 0; i < old.length; i++) { old[i].classList.remove('in-range'); }
		if (!r) { return; }
		var all = ports(), on = false;
		for (var j = 0; j < all.length; j++) {
			if (all[j].value === r[0]) { on = true; }
			if (on) { all[j].classList.add('in-range'); }
			if (all[j].value === r[1]) { break; }
		}
	}
	function showRange(start, end) {
		open(start, false);
		markRange([start, end || start]);
		var p = portFor(start);
		if (p) {
			p.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
			p.focus({ preventScroll: true });
		}
	}

	// ---- "Free only": every port that is not free fades back. Remembered
	// per browser, since someone hunting for addresses wants it each time.
	var wrap = document.querySelector('.patch-wrap');
	function setFreeOnly(on) {
		if (!wrap) { return; }
		wrap.toggleAttribute('data-free-only', on);
		var b = document.querySelector('[data-free-only]');
		if (b) { b.setAttribute('aria-pressed', on ? 'true' : 'false'); }
		try { localStorage.setItem('netis-free-only', on ? '1' : ''); } catch (e) {}
	}
	try { if (localStorage.getItem('netis-free-only') === '1') { setFreeOnly(true); } } catch (e) {}

	// ---- the hover card: one element for the whole panel, filled from the
	// grid's JSON (#grid-data) for the port under the pointer or, from the
	// keyboard, the focused one. It repeats what the port's accessible name
	// already says, so it is hidden from screen readers.
	var card = document.getElementById('port-card');
	var admin = card && card.hasAttribute('data-admin');
	var cards = null, cardPort = null, showTimer = null, warmUntil = 0;
	var hoverOK = window.matchMedia && window.matchMedia('(hover: hover) and (pointer: fine)').matches;
	var states = {
		online: 'Online', offline: 'Offline', reserved: 'Not seen yet', conflict: 'IP conflict',
		free: 'Free', pool: 'Free, in DHCP pool'
	};

	function cardData() {
		if (cards) { return cards; }
		var el = document.getElementById('grid-data');
		try { cards = el ? JSON.parse(el.textContent) : {}; } catch (e) { cards = {}; }
		return cards;
	}
	function stateOf(p) {
		for (var k in states) { if (p.classList.contains(k)) { return k; } }
		return p.classList.contains('edge') ? 'edge' : '';
	}
	function el(tag, cls, text) {
		var n = document.createElement(tag);
		if (cls) { n.className = cls; }
		if (text) { n.textContent = text; }
		return n;
	}
	function fillCard(p) {
		var ip = p.value, st = stateOf(p), d = cardData()[ip];
		card.textContent = '';
		card.setAttribute('data-state', st);
		var head = el('div', 'pc-head');
		head.appendChild(el('span', 'pc-ip mono', ip));
		if (p.classList.contains('static')) { head.appendChild(el('span', 'chip static', 'Static')); }
		if (p.classList.contains('dhcp')) { head.appendChild(el('span', 'chip dhcp', 'DHCP')); }
		card.appendChild(head);
		if (st === 'edge') {
			var lbl = p.getAttribute('aria-label') || '';
			card.appendChild(el('div', 'pc-note', lbl.slice(ip.length + 1) || 'Not assignable'));
			return;
		}
		var line = el('div', 'pc-state');
		line.appendChild(el('span', 'led ' + st));
		line.appendChild(el('span', '', states[st] || st));
		card.appendChild(line);
		if (d) {
			card.appendChild(el('div', 'pc-name', d.n));
			if (d.c > 1) { card.appendChild(el('div', 'pc-warn', d.c + ' devices claim this address')); }
			if (d.m) { card.appendChild(el('div', 'pc-meta mono', d.m)); }
			card.appendChild(el('div', 'pc-meta', d.s ? 'Last seen ' + d.s : 'Never seen by a scan'));
		}
		var hint = el('div', 'pc-hint');
		hint.appendChild(document.createTextNode(st === 'free' && admin ? 'Click to add a device · ' : 'Click for details · '));
		hint.appendChild(el('kbd', '', 'C'));
		hint.appendChild(document.createTextNode(' copy'));
		card.appendChild(hint);
	}
	// Below the port, or above it when there is no room underneath; kept
	// inside the window at the sides, with the arrow still on the port.
	function placeCard(p) {
		var r = p.getBoundingClientRect(), gap = 8, edge = 8;
		var w = card.offsetWidth, h = card.offsetHeight;
		var below = r.bottom + gap + h <= window.innerHeight - edge || r.top - gap - h < edge;
		var x = Math.min(Math.max(r.left + r.width / 2 - w / 2, edge), window.innerWidth - w - edge);
		var y = below ? r.bottom + gap : r.top - gap - h;
		card.style.left = Math.round(x) + 'px';
		card.style.top = Math.round(y) + 'px';
		card.style.setProperty('--ax', Math.round(r.left + r.width / 2 - x) + 'px');
		card.setAttribute('data-side', below ? 'bottom' : 'top');
	}
	function showCard(p) {
		if (!card) { return; }
		clearTimeout(showTimer);
		cardPort = p;
		fillCard(p);
		card.hidden = false;
		placeCard(p);
		card.setAttribute('data-show', '');
	}
	function hideCard() {
		clearTimeout(showTimer);
		showTimer = null;
		if (!card || card.hidden) { cardPort = null; return; }
		warmUntil = Date.now() + 400;
		cardPort = null;
		card.removeAttribute('data-show');
		card.hidden = true;
	}
	// The first card waits a moment, so a pointer passing over the panel
	// does not flicker; once one is up, moving along the ports switches it
	// at once.
	function wantCard(p) {
		if (p === cardPort) { return; }
		clearTimeout(showTimer);
		if (cardPort || Date.now() < warmUntil) { showCard(p); return; }
		showTimer = setTimeout(function () { showCard(p); }, 180);
	}
	function portAt(t) {
		return t && t.closest ? t.closest('form.patch .port') : null;
	}
	if (card && hoverOK) {
		document.addEventListener('mouseover', function (e) {
			var p = portAt(e.target);
			if (p) { wantCard(p); } else if (cardPort || showTimer) { hideCard(); }
		});
		document.addEventListener('mouseleave', hideCard);
	}
	if (card) {
		document.addEventListener('focusin', function (e) {
			var p = portAt(e.target);
			if (p && p.matches(':focus-visible')) { showCard(p); }
		});
		document.addEventListener('focusout', function (e) {
			if (portAt(e.target) && cardPort === e.target) { hideCard(); }
		});
		window.addEventListener('scroll', hideCard, { passive: true, capture: true });
		window.addEventListener('resize', hideCard);
		// C copies the address under the pointer too, not only the focused one.
		document.addEventListener('keydown', function (e) {
			if (!cardPort || portAt(e.target) || e.ctrlKey || e.metaKey || e.altKey) { return; }
			if (e.key !== 'c' && e.key !== 'C') { return; }
			if (e.target.closest && e.target.closest('input, textarea, select, [contenteditable], dialog')) { return; }
			e.preventDefault();
			if (window.netisCopy) { window.netisCopy(cardPort.value); }
		});
	}

	// A live refresh replaced the ports: put back the selection, the tab
	// stop and focus, and bring the open details up to date. A lease change
	// in the details reloads them too.
	var refocus = null;
	document.addEventListener('htmx:beforeSwap', function (e) {
		if (e.detail.target && e.detail.target.id === 'grid') {
			var a = document.activeElement;
			refocus = a && a.classList && a.classList.contains('port') ? a.value : null;
		}
	});
	document.addEventListener('htmx:afterSwap', function (e) {
		if (!e.detail.target || e.detail.target.id !== 'grid') { return; }
		cards = null;
		if (cardPort) {
			var hp = portFor(cardPort.value);
			if (hp) { showCard(hp); } else { hideCard(); }
		}
		markSelected(selected);
		markRange(range);
		roving(refocus || selected);
		if (refocus) {
			var p = portFor(refocus);
			if (p) { p.focus({ preventScroll: true }); }
			refocus = null;
		}
		if (selected && !panel.contains(document.activeElement)) { loadPanel(selected, false); }
	});
	document.addEventListener('htmx:afterRequest', function (e) {
		if (e.detail.successful && e.detail.elt && e.detail.elt.hasAttribute &&
			e.detail.elt.hasAttribute('data-cell-reload') && selected) {
			loadPanel(selected, false);
		}
	});

	roving(selected);
	if (selected) {
		var p = portFor(selected);
		if (p) { p.scrollIntoView({ block: 'nearest' }); }
	}
})();
