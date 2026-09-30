// Devices page: list or tiles, the column picker, the filter bar on phones,
// and row selection for the bulk actions. Everything here is an enhancement:
// without it the list shows its default columns, the filters are a plain GET
// form with an Apply button, and the bulk bar is always shown.
(function () {
	var root = document.getElementById('devices');
	if (!root) { return; }
	var VIEW = 'netis-devices-view', COLS = 'netis-devices-cols';
	var OPTIONAL = ['mac', 'lease', 'func', 'tags'];
	// What shows until someone picks columns; the server renders the same
	// (show-tags on #devices), so the list looks the same without script.
	var DEFAULT_COLS = ['tags'];
	var phone = window.matchMedia('(max-width: 640px)');

	function get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } }
	function put(k, v) { try { localStorage.setItem(k, v); } catch (e) {} }
	function all(sel) { return Array.prototype.slice.call(root.querySelectorAll(sel)); }

	function chosenCols() {
		var v = get(COLS);
		if (v === null) { return DEFAULT_COLS.slice(); }
		return v.split(',').filter(function (c) { return OPTIONAL.indexOf(c) >= 0; });
	}
	function applyCols() {
		var on = chosenCols();
		OPTIONAL.forEach(function (c) { root.classList.toggle('show-' + c, on.indexOf(c) >= 0); });
		all('[data-col]').forEach(function (b) { b.checked = on.indexOf(b.getAttribute('data-col')) >= 0; });
	}

	function view() { return get(VIEW) === 'grid' ? 'grid' : 'list'; }
	function applyView() {
		var v = view(), list = document.getElementById('dev-list'), grid = document.getElementById('dev-grid');
		if (list && grid) {
			list.hidden = v === 'grid';
			grid.hidden = v !== 'grid';
		}
		root.classList.toggle('view-grid', v === 'grid');
		all('#dev-view button').forEach(function (b) {
			var on = b.getAttribute('data-view') === v;
			b.classList.toggle('on', on);
			b.setAttribute('aria-pressed', on ? 'true' : 'false');
		});
	}

	function boxes() { return all('input[name="id"][form="bulk"]'); }
	function selected() { return boxes().filter(function (b) { return b.checked; }).length; }
	function applySelection() {
		var bar = document.getElementById('bulk');
		if (!bar) { return; }
		var n = selected(), total = boxes().length, head = document.getElementById('dl-all');
		bar.toggleAttribute('data-empty', n === 0);
		var label = bar.querySelector('.dl-selected');
		if (label) { label.textContent = n + ' selected'; }
		if (head) {
			head.checked = n > 0 && n === total;
			head.indeterminate = n > 0 && n < total;
		}
	}

	// The Filters disclosure is open on wide screens and folded on phones.
	function applyFold() {
		var more = root.querySelector('.dl-more');
		if (more) { more.open = !phone.matches; }
	}
	function filterCount() {
		var form = document.getElementById('dev-filters'), badge = root.querySelector('.dl-fcount');
		if (!form || !badge) { return; }
		var n = 0;
		Array.prototype.forEach.call(form.querySelectorAll('select'), function (s) { if (s.value) { n++; } });
		Array.prototype.forEach.call(form.querySelectorAll('.dl-toggle input'), function (c) { if (c.checked) { n++; } });
		badge.textContent = n;
		badge.hidden = n === 0;
	}

	function setup() {
		all('#dev-view, .dl-cols, #dl-all').forEach(function (el) { el.hidden = false; });
		all('.dl-apply').forEach(function (el) { el.hidden = true; });
		applyCols();
		applyView();
		applySelection();
	}

	setup();
	applyFold();
	if (phone.addEventListener) { phone.addEventListener('change', applyFold); }

	// The column picker and view toggle are part of the results, which the
	// filter bar replaces, so they are handled here once for all of them.
	root.addEventListener('click', function (e) {
		var b = e.target.closest && e.target.closest('#dev-view button');
		if (!b) { return; }
		put(VIEW, b.getAttribute('data-view'));
		applyView();
	});

	var form = document.getElementById('dev-filters');
	if (form) { form.addEventListener('input', filterCount); }

	root.addEventListener('change', function (e) {
		var t = e.target;
		if (t.hasAttribute('data-col')) {
			put(COLS, all('[data-col]').filter(function (x) { return x.checked; })
				.map(function (x) { return x.getAttribute('data-col'); }).join(','));
			applyCols();
		} else if (t.id === 'dl-all') {
			boxes().forEach(function (b) { b.checked = t.checked; });
			applySelection();
		} else if (t.name === 'id' && t.getAttribute('form') === 'bulk') {
			applySelection();
		}
	});

	// Bulk actions: nothing selected does nothing, Add tag needs a tag, and
	// Delete asks first (the server asks on a page of its own otherwise).
	root.addEventListener('submit', function (e) {
		// getAttribute, not f.id: the form's own "id" fields shadow that property.
		var f = e.target;
		if (f.getAttribute('id') !== 'bulk') { return; }
		var n = selected(), s = e.submitter;
		if (n === 0) { e.preventDefault(); return; }
		var action = s ? s.getAttribute('formaction') || '' : '';
		if (action.indexOf('/tag') > 0) {
			var tag = f.querySelector('input[name="tag"]');
			if (tag && !tag.value.trim()) { e.preventDefault(); tag.focus(); }
			return;
		}
		if (s && s.hasAttribute('data-confirm')) {
			if (!window.confirm('Delete ' + n + (n === 1 ? ' device' : ' devices') + '? This cannot be undone.')) {
				e.preventDefault();
				return;
			}
			var c = document.createElement('input');
			c.type = 'hidden';
			c.name = 'confirm';
			c.value = '1';
			f.appendChild(c);
		}
	});

	// Leave blank fields out of the URL htmx requests and pushes.
	document.body.addEventListener('htmx:configRequest', function (e) {
		if (!e.detail.elt || e.detail.elt.id !== 'dev-filters') { return; }
		var fd = e.detail.formData, drop = [];
		if (!fd) { return; }
		fd.forEach(function (v, k) { if (v === '') { drop.push(k); } });
		drop.forEach(function (k) { fd.delete(k); });
	});
	document.body.addEventListener('htmx:afterSettle', setup);
})();
