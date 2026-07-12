(function () {
	// Per-kind default icons — mirrors views.kindIcons. Used to keep the icon
	// picker's selection in sync with the Kind field until the user overrides.
	var KIND_ICON = {
		computer: '💻', switch: '🔀', phone: '📱', server: '🖥️', printer: '🖨️',
		iot: '💡', vm: '🧊', lxc: '📦', 'wg-peer': '🔒', other: '❓'
	};

	function closeModal() {
		var m = document.getElementById('modal');
		if (m) { m.innerHTML = ''; }
	}

	// Close on ✕ / Cancel ([data-close]) or a click on the scrim backdrop itself.
	document.addEventListener('click', function (e) {
		if (e.target.closest('[data-close]')) { closeModal(); return; }
		if (e.target.classList && e.target.classList.contains('dialog-scrim')) { closeModal(); }
	});

	// Close on Escape when a dialog is open.
	document.addEventListener('keydown', function (e) {
		if (e.key === 'Escape' && document.querySelector('#modal .dialog')) { closeModal(); }
	});

	// Icon picker: click a swatch to select it.
	document.addEventListener('click', function (e) {
		var sw = e.target.closest('.ic-swatch');
		if (!sw) { return; }
		var pick = sw.closest('.iconpick');
		if (!pick) { return; }
		e.preventDefault();
		selectSwatch(pick, sw.dataset.icon);
	});

	// Kind change: if the icon is still the previous kind's default (untouched),
	// switch it to the new kind's default; always remember the new default.
	document.addEventListener('change', function (e) {
		if (!e.target.matches || !e.target.matches('select[name=kind]')) { return; }
		var dialog = e.target.closest('.dialog');
		if (!dialog) { return; }
		var pick = dialog.querySelector('.iconpick');
		var hidden = dialog.querySelector('input[name=icon]');
		if (!pick || !hidden) { return; }
		var newDefault = KIND_ICON[e.target.value] || '❓';
		if (hidden.value === pick.dataset.kindDefault) {
			selectSwatch(pick, newDefault);
		}
		pick.dataset.kindDefault = newDefault;
	});

	function selectSwatch(pick, icon) {
		var hidden = pick.parentNode.querySelector('input[name=icon]') ||
			pick.closest('.dialog').querySelector('input[name=icon]');
		if (hidden) { hidden.value = icon; }
		pick.querySelectorAll('.ic-swatch').forEach(function (b) {
			b.classList.toggle('selected', b.dataset.icon === icon);
		});
	}
})();
