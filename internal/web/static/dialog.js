(function () {
	// Per-kind default icons — mirrors views.kindIcons. Used to keep the icon
	// picker's selection in sync with the Kind field until the user overrides.
	var KIND_ICON = {
		computer: 'laptop', switch: 'ethernet-port', router: 'router', modem: 'signal',
		phone: 'smartphone', server: 'server', printer: 'printer', iot: 'lightbulb',
		vm: 'box', lxc: 'container', 'wg-peer': 'lock', other: 'circle-help'
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

	// Close on the X / Cancel ([data-close]) or a click on the backdrop, which
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
		var newDefault = KIND_ICON[e.target.value] || 'circle-help';
		if (hidden.value === pick.dataset.kindDefault) {
			selectSwatch(pick, newDefault);
		}
		pick.dataset.kindDefault = newDefault;
	});

	// Copy buttons ([data-copy] naming an input's id) copy that input's
	// value. They start hidden, since without JS there is nothing to click and
	// the text itself stays selectable. The Clipboard API needs a secure
	// context, which a plain-http LAN install is not, so fall back to
	// selecting the text and execCommand.
	document.querySelectorAll('[data-copy][hidden]').forEach(function (b) { b.hidden = false; });
	document.addEventListener('click', function (e) {
		var b = e.target.closest && e.target.closest('[data-copy]');
		if (!b) { return; }
		var src = document.getElementById(b.getAttribute('data-copy'));
		if (!src) { return; }
		var done = function () {
			b.textContent = 'Copied';
			setTimeout(function () { b.textContent = 'Copy'; }, 2000);
		};
		var legacy = function () {
			src.focus();
			src.select();
			try { if (document.execCommand('copy')) { done(); } } catch (err) {}
		};
		if (navigator.clipboard && window.isSecureContext) {
			navigator.clipboard.writeText(src.value).then(done, legacy);
		} else {
			legacy();
		}
	});

	// Read-only fields marked data-select-all select their whole value on
	// focus, so a token can be copied by hand in one go.
	document.addEventListener('focusin', function (e) {
		if (e.target.matches && e.target.matches('input[data-select-all]')) { e.target.select(); }
	});

	// Device form: "Use detected" ([data-fill]) puts the detected value in
	// its field, and "Add detected tag" ([data-append-tag]) adds a tag to the
	// tags field. The events let the Kind change keep the icon in step.
	document.addEventListener('click', function (e) {
		var b = e.target.closest && e.target.closest('[data-fill], [data-append-tag]');
		if (!b || !b.form) { return; }
		e.preventDefault();
		if (b.hasAttribute('data-fill')) {
			var el = b.form.elements[b.getAttribute('data-fill')];
			if (!el) { return; }
			el.value = b.getAttribute('data-value');
			el.dispatchEvent(new Event('input', { bubbles: true }));
			el.dispatchEvent(new Event('change', { bubbles: true }));
		} else {
			var tags = b.form.elements.tags;
			if (!tags) { return; }
			var tag = b.getAttribute('data-append-tag');
			var have = tags.value.split(',').map(function (t) { return t.trim().toLowerCase(); });
			if (have.indexOf(tag.toLowerCase()) < 0) {
				tags.value = tags.value.trim() === '' ? tag : tags.value.replace(/[\s,]*$/, '') + ', ' + tag;
				tags.dispatchEvent(new Event('input', { bubbles: true }));
			}
		}
		b.hidden = true;
	});

	function selectSwatch(pick, icon) {
		var hidden = pick.parentNode.querySelector('input[name=icon]') ||
			pick.closest('.dialog').querySelector('input[name=icon]');
		if (hidden) { hidden.value = icon; }
		pick.querySelectorAll('.ic-swatch').forEach(function (b) {
			b.classList.toggle('selected', b.dataset.icon === icon);
			b.setAttribute('aria-pressed', b.dataset.icon === icon ? 'true' : 'false');
		});
		// The picker's summary shows the icon the device will get.
		var disc = pick.closest('.iconpick-disc');
		var use = disc && disc.querySelector('.icon-preview use');
		if (use) { use.setAttribute('href', '/static/icons.svg#' + icon); }
	}

	// Device form: typing an IP chooses the subnet that holds it (the most
	// specific one), unless the user has chosen one by hand. IPv4 only.
	function ip4(s) {
		var m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s.trim());
		if (!m) { return null; }
		var n = 0;
		for (var i = 1; i <= 4; i++) {
			var o = +m[i];
			if (o > 255) { return null; }
			n = n * 256 + o;
		}
		return n;
	}
	function cidrBits(ip, cidr) {
		var parts = cidr.split('/');
		var base = ip4(parts[0]);
		var bits = +parts[1];
		if (base === null || isNaN(bits) || bits < 0 || bits > 32) { return -1; }
		var size = Math.pow(2, 32 - bits);
		return Math.floor(ip / size) === Math.floor(base / size) ? bits : -1;
	}
	document.addEventListener('input', function (e) {
		if (!e.target.matches || !e.target.matches('.device-form input[name=ip]')) { return; }
		var sel = e.target.form.querySelector('select[name=subnet_id]');
		if (!sel || sel.dataset.userChosen) { return; }
		var ip = ip4(e.target.value);
		if (ip === null) { return; }
		var best = null, bestBits = -1;
		Array.prototype.forEach.call(sel.options, function (o) {
			if (!o.dataset.cidr) { return; }
			var b = cidrBits(ip, o.dataset.cidr);
			if (b > bestBits) { best = o; bestBits = b; }
		});
		if (best) { sel.value = best.value; }
	});
	document.addEventListener('change', function (e) {
		if (e.target.matches && e.target.matches('.device-form select[name=subnet_id]')) {
			e.target.dataset.userChosen = '1';
		}
	});

	// Device form: the parent picker's filter box narrows the list by name
	// or IP. Without JavaScript the box stays hidden and the list is whole.
	function armFilters(root) {
		root.querySelectorAll('.df-filter[hidden]').forEach(function (f) { f.hidden = false; });
	}
	armFilters(document);
	document.addEventListener('htmx:afterSwap', function (e) { armFilters(e.detail.target || document); });
	document.addEventListener('input', function (e) {
		if (!e.target.matches || !e.target.matches('.df-filter')) { return; }
		var sel = document.getElementById(e.target.getAttribute('data-filter-for'));
		if (!sel) { return; }
		var q = e.target.value.trim().toLowerCase();
		Array.prototype.forEach.call(sel.options, function (o) {
			o.hidden = q !== '' && o.value !== '' && !o.selected && o.text.toLowerCase().indexOf(q) < 0;
		});
		sel.querySelectorAll('optgroup').forEach(function (g) {
			g.hidden = !Array.prototype.some.call(g.children, function (o) { return !o.hidden; });
		});
	});
})();
