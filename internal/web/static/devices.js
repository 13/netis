(function () {
	function stored() {
		try { return localStorage.getItem('netis-devices-view') || 'list'; } catch (e) { return 'list'; }
	}
	function show(view) {
		var list = document.getElementById('dev-list');
		var grid = document.getElementById('dev-grid');
		if (!list || !grid) { return; }
		list.style.display = view === 'grid' ? 'none' : '';
		grid.style.display = view === 'grid' ? '' : 'none';
		document.querySelectorAll('#dev-view button').forEach(function (b) {
			b.classList.toggle('on', b.getAttribute('data-view') === view);
		});
	}
	show(stored());
	document.querySelectorAll('#dev-view button').forEach(function (b) {
		b.addEventListener('click', function () {
			var v = b.getAttribute('data-view');
			try { localStorage.setItem('netis-devices-view', v); } catch (e) {}
			show(v);
		});
	});
})();
