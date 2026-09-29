(function () {
	// Keeps every <time datetime> rendered by the Time component current: the
	// text is the relative time in the same words the server uses (see
	// relTime in views/helpers.go) and the title the absolute time in the
	// reader's own time zone. Without JavaScript the server's text and UTC
	// title stay.
	function span(ms) {
		var m = Math.floor(ms / 60000);
		if (m < 60) { return m + 'm'; }
		var h = Math.floor(m / 60);
		if (h < 24) { return h + 'h'; }
		return Math.floor(h / 24) + 'd';
	}
	function rel(t) {
		var d = Date.now() - t;
		if (d < 0) {
			return -d < 60000 ? 'in a moment' : 'in ' + span(-d);
		}
		return d < 60000 ? 'just now' : span(d) + ' ago';
	}
	var fmt = null;
	try {
		fmt = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });
	} catch (e) {}
	function update(root, titles) {
		var els = Array.prototype.slice.call(root.querySelectorAll('time[datetime]'));
		if (root.matches && root.matches('time[datetime]')) { els.push(root); }
		els.forEach(function (el) {
			var t = Date.parse(el.getAttribute('datetime'));
			if (isNaN(t)) { return; }
			el.textContent = rel(t);
			if (titles && fmt) { el.title = fmt.format(new Date(t)); }
		});
	}
	update(document, true);
	setInterval(function () { update(document, false); }, 60000);
	// Content swapped in by htmx (dashboard refresh, dialogs) gets local titles too.
	document.addEventListener('htmx:load', function (e) { update(e.target, true); });
})();
