(function () {
	function current() {
		// Dark is the default; the bootstrap has already set data-theme.
		return document.documentElement.getAttribute('data-theme') || 'dark';
	}
	function label(t) {
		var l = document.querySelector('#theme-toggle .tglabel');
		if (l) { l.textContent = t === 'dark' ? 'Dark' : 'Light'; }
	}
	function apply(t) {
		document.documentElement.setAttribute('data-theme', t);
		try { localStorage.setItem('netis-theme', t); } catch (e) {}
		label(t);
	}
	var btn = document.getElementById('theme-toggle');
	if (btn) {
		// Reflect the current theme in the label without writing storage — the OS
		// preference keeps being followed until the user explicitly toggles.
		label(current());
		btn.addEventListener('click', function () { apply(current() === 'dark' ? 'light' : 'dark'); });
	}
	// active-nav: longest-prefix match ("/" only exact) so no server change is needed.
	var path = location.pathname;
	document.querySelectorAll('nav.top .links a').forEach(function (a) {
		var href = a.getAttribute('href');
		var active = href === '/' ? path === '/' : path.indexOf(href) === 0;
		if (active) { a.classList.add('active'); }
	});
})();
