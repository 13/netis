(function () {
	// Per-kind default icons — mirrors views.kindIcons. Used to keep the icon
	// picker's selection in sync with the Kind field until the user overrides.
	var KIND_ICON = {
		computer: '💻', switch: '🔀', router: '🛜', modem: '📶', phone: '📱',
		server: '🖥️', printer: '🖨️', iot: '💡', vm: '🧊', lxc: '📦',
		'wg-peer': '🔒', other: '❓'
	};

	// The control that opened the current dialog, to hand focus back to when
	// it closes. A dialog swapped in from inside another (Edit on a grid
	// cell) keeps the original opener, since the button it came from is gone.
	var opener = null;

	// Dialog fragments arrive in #modal as a closed <dialog>. showModal() puts
	// it in the top layer, makes the rest of the page inert (so Tab and screen
	// readers stay inside) and focuses its autofocus field or first control.
	function openModal() {
		var m = document.getElementById('modal');
		var d = m && m.querySelector('dialog');
		if (!d || d.open) { return; }
		var a = document.activeElement;
		if (a && a !== document.body && !m.contains(a)) { opener = a; }
		if (typeof d.showModal === 'function') { d.showModal(); } else { d.setAttribute('open', ''); }
	}

	function cleanup() {
		var m = document.getElementById('modal');
		if (m) { m.innerHTML = ''; }
		if (opener && opener.isConnected) { opener.focus(); }
		opener = null;
	}

	// Closing goes through dialog.close() so Escape (which closes a modal
	// dialog natively) and the buttons below end in the same cleanup.
	function closeModal() {
		var d = document.querySelector('#modal dialog');
		if (d && d.open && typeof d.close === 'function') { d.close(); return; }
		cleanup();
	}

	document.addEventListener('close', function (e) {
		if (e.target.matches && e.target.matches('#modal dialog')) { cleanup(); }
	}, true);

	document.addEventListener('htmx:afterSwap', function (e) {
		if (e.detail.target && e.detail.target.id === 'modal') { openModal(); }
	});

	// htmx leaves 4xx responses unswapped. A form in a dialog answers a
	// validation failure or conflict with the dialog again, message included,
	// so let that one through.
	document.addEventListener('htmx:beforeSwap', function (e) {
		var st = e.detail.xhr && e.detail.xhr.status;
		if (e.detail.target && e.detail.target.id === 'modal' && (st === 400 || st === 409)) {
			e.detail.shouldSwap = true;
			e.detail.isError = false;
		}
	});

	// Close on ✕ / Cancel ([data-close]) or a click on the backdrop, which
	// reports the <dialog> itself as the target (its content fills the box).
	document.addEventListener('click', function (e) {
		if (e.target.closest('[data-close]')) { closeModal(); return; }
		if (e.target.matches && e.target.matches('#modal dialog')) { closeModal(); }
	});

	// Forms that delete or sign something out carry data-confirm and post only
	// once the user agrees. Capture phase, so nothing else acts on a refused
	// submit.
	document.addEventListener('submit', function (e) {
		var f = e.target.closest && e.target.closest('form[data-confirm]');
		if (f && !window.confirm(f.getAttribute('data-confirm'))) {
			e.preventDefault();
			e.stopImmediatePropagation();
		}
	}, true);

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
			b.setAttribute('aria-pressed', b.dataset.icon === icon ? 'true' : 'false');
		});
	}
})();
