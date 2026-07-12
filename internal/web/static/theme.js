(function () {
	function current() {
		return document.documentElement.getAttribute('data-theme') ||
			(matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark');
	}
	function apply(t) {
		document.documentElement.setAttribute('data-theme', t);
		try { localStorage.setItem('netis-theme', t); } catch (e) {}
		var l = document.querySelector('#theme-toggle .tglabel');
		if (l) { l.textContent = t === 'dark' ? 'Dark' : 'Light'; }
	}
	var btn = document.getElementById('theme-toggle');
	if (btn) {
		apply(current());
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
