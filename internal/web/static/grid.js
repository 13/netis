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
			if (selected) { e.preventDefault(); close(); }
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

	// A port's name as a hover tooltip, set on first hover rather than
	// printed into every port.
	document.addEventListener('mouseover', function (e) {
		var p = e.target;
		if (p.classList && p.classList.contains('port') && !p.title && p.closest('form.patch')) {
			p.title = p.getAttribute('aria-label');
		}
	});

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
		markSelected(selected);
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
