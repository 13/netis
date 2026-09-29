(function () {
	// An explicit choice made in the account menu, or null when the user
	// never made one (or chose "system") and the OS preference decides.
	function stored() {
		try {
			var t = localStorage.getItem('netis-theme');
			return t === 'light' || t === 'dark' ? t : null;
		} catch (e) { return null; }
	}
	var mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: light)') : null;
	function system() { return mq && mq.matches ? 'light' : 'dark'; }
	// Mark the current choice in every theme control (sidebar menu and the
	// mobile More sheet both carry one).
	function mark() {
		var choice = stored() || 'system';
		document.querySelectorAll('[data-theme-set]').forEach(function (b) {
			var on = b.getAttribute('data-theme-set') === choice;
			b.classList.toggle('on', on);
			b.setAttribute('aria-pressed', on ? 'true' : 'false');
		});
	}
	// The browser chrome follows the page: with a theme picked, both
	// theme-color metas (one per OS scheme) take that theme's background;
	// with none, each goes back to its own scheme's.
	var chrome = { light: '#ECEEEC', dark: '#161A18' };
	function tint() {
		var t = stored();
		document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
			if (!m.dataset.orig) { m.dataset.orig = m.getAttribute('content'); }
			m.setAttribute('content', t ? chrome[t] : m.dataset.orig);
		});
	}
	function apply() {
		document.documentElement.setAttribute('data-theme', stored() || system());
		mark();
		tint();
	}
	// choose sets the theme to "light", "dark" or "system" (follow the OS).
	function choose(t) {
		try {
			if (t === 'light' || t === 'dark') { localStorage.setItem('netis-theme', t); } else { localStorage.removeItem('netis-theme'); }
		} catch (e) {}
		apply();
	}
	window.netisTheme = choose;
	// Until the user picks a theme, follow the OS as it changes (e.g. a
	// scheduled switch to dark at sunset).
	if (mq) {
		if (mq.addEventListener) { mq.addEventListener('change', apply); } else if (mq.addListener) { mq.addListener(apply); }
	}
	document.querySelectorAll('[data-theme-choice]').forEach(function (el) { el.hidden = false; });
	mark();
	tint();
	document.addEventListener('click', function (e) {
		var b = e.target.closest('[data-theme-set]');
		if (b) { choose(b.getAttribute('data-theme-set')); }
	});
	// Close an open menu (account, More) on Escape or a click outside it.
	document.addEventListener('click', function (e) {
		document.querySelectorAll('details.acct[open], details.more[open]').forEach(function (d) {
			if (!d.contains(e.target)) { d.open = false; }
		});
	});
	document.addEventListener('keydown', function (e) {
		if (e.key !== 'Escape') { return; }
		document.querySelectorAll('details.acct[open], details.more[open]').forEach(function (d) {
			d.open = false;
			var s = d.querySelector('summary');
			if (s) { s.focus(); }
		});
	});
})();
