(function () {
	'use strict';

	// Tag chip editor for the device form. The form's tags text input
	// ([data-tag-editor]) stays the fallback without JavaScript and is still
	// what the form posts: this hides it, shows its tags as chips with a text
	// box after them, and writes the chips back into it as a comma list after
	// every change. Existing tags, with their colours already resolved, come
	// from the input's data-tags JSON and feed the suggestions.

	// Palette keys in picker order, and the auto hue: FNV-1a 32-bit over the
	// lower-cased name, mod 7. Keep in step with views.TagPalette and
	// views.TagColor (e2e/tests/tags.spec.ts checks a known pair).
	var PALETTE = ['slate', 'indigo', 'sky', 'teal', 'violet', 'pink', 'sand'];
	var MAX_LEN = 64; // characters, as the server allows
	var MAX_SUGGEST = 8;
	var seq = 0;

	function autoColor(name) {
		var bytes = new TextEncoder().encode(name.toLowerCase());
		var h = 0x811c9dc5;
		for (var i = 0; i < bytes.length; i++) {
			h ^= bytes[i];
			h = Math.imul(h, 0x01000193) >>> 0;
		}
		return PALETTE[h % PALETTE.length];
	}

	// clean trims a typed tag and cuts it to MAX_LEN characters (not UTF-16
	// units, so an emoji is not split).
	function clean(s) {
		var chars = Array.from(s.trim());
		return chars.length > MAX_LEN ? chars.slice(0, MAX_LEN).join('').trim() : chars.join('');
	}

	function icon(name) {
		var ns = 'http://www.w3.org/2000/svg';
		var svg = document.createElementNS(ns, 'svg');
		svg.setAttribute('class', 'icon');
		svg.setAttribute('aria-hidden', 'true');
		var use = document.createElementNS(ns, 'use');
		use.setAttribute('href', '/static/icons.svg#' + name);
		svg.appendChild(use);
		return svg;
	}

	function init(input) {
		if (input._tagEditor) { return; }
		// A page restored from htmx's history cache brings back the editor's
		// markup without its handlers: drop it and build afresh.
		var stale = input.nextElementSibling;
		if (stale && stale.classList.contains('tag-editor')) { stale.remove(); }

		var id = input.id || ('tag-editor-' + (++seq));
		var listId = id + '-list';
		var known = {};
		try {
			(JSON.parse(input.getAttribute('data-tags') || '[]') || []).forEach(function (t) {
				known[t.name.toLowerCase()] = t;
			});
		} catch (err) {}

		var chips = [];
		var matches = [];
		var active = -1;

		var editor = document.createElement('div');
		editor.className = 'tag-editor';
		var box = document.createElement('input');
		box.type = 'text';
		box.id = id + '-input';
		box.className = 'tag-editor-input';
		box.autocomplete = 'off';
		box.setAttribute('role', 'combobox');
		box.setAttribute('aria-expanded', 'false');
		box.setAttribute('aria-controls', listId);
		box.setAttribute('aria-autocomplete', 'list');
		box.setAttribute('aria-label', 'Add a tag');
		var list = document.createElement('ul');
		list.id = listId;
		list.className = 'tag-editor-list';
		list.setAttribute('role', 'listbox');
		list.setAttribute('aria-label', 'Existing tags');
		list.hidden = true;
		// The form scrolls inside its panel, which would clip the list: as a
		// popover it goes in the top layer, above the dialog too.
		var popover = typeof list.showPopover === 'function';
		if (popover) { list.setAttribute('popover', 'manual'); }
		editor.appendChild(box);
		editor.appendChild(list);

		var hint = input.nextElementSibling;
		if (hint && hint.classList.contains('hint')) { hint.hidden = true; }
		input.after(editor);
		input.hidden = true;
		var label = input.id && document.querySelector('label[for="' + input.id + '"]');
		if (label) { label.htmlFor = box.id; }

		function colorOf(name) {
			var k = known[name.toLowerCase()];
			return k && PALETTE.indexOf(k.color) >= 0 ? k.color : autoColor(name);
		}

		function has(name) {
			var n = name.toLowerCase();
			return chips.some(function (c) { return c.toLowerCase() === n; });
		}

		function chipEl(name) {
			var chip = document.createElement('span');
			chip.className = 'tag tag-' + colorOf(name);
			chip.title = name;
			var text = document.createElement('span');
			text.className = 'tag-name';
			text.textContent = name;
			var rm = document.createElement('button');
			rm.type = 'button';
			rm.className = 'tag-remove';
			rm.setAttribute('aria-label', 'Remove tag ' + name);
			rm.appendChild(icon('x'));
			rm.addEventListener('click', function (e) {
				e.preventDefault();
				remove(chips.indexOf(name));
				box.focus();
			});
			chip.appendChild(text);
			chip.appendChild(rm);
			return chip;
		}

		function render() {
			// :scope > .tag only: the suggestion list is a child of the editor
			// too, and its options are also .tag chips.
			editor.querySelectorAll(':scope > .tag').forEach(function (c) { c.remove(); });
			chips.forEach(function (name) { editor.insertBefore(chipEl(name), box); });
			box.placeholder = chips.length ? '' : (input.placeholder || '');
		}

		function save() {
			input.value = chips.join(', ');
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new Event('change', { bubbles: true }));
		}

		// add puts each comma-separated part of text on as a chip, skipping
		// empty parts and ones already present. A known tag keeps its casing.
		function add(text) {
			var changed = false;
			String(text).split(',').forEach(function (part) {
				var name = clean(part);
				if (name === '' || has(name)) { return; }
				var k = known[name.toLowerCase()];
				chips.push(k ? k.name : name);
				changed = true;
			});
			if (changed) { render(); save(); }
			return changed;
		}

		function remove(i) {
			if (i < 0 || i >= chips.length) { return; }
			chips.splice(i, 1);
			render();
			save();
		}

		function commitBox() {
			var text = box.value;
			box.value = '';
			close();
			return add(text);
		}

		// place puts the popover list under the field, or over it when there
		// is no room below. It is the target of the window resize/scroll
		// listeners added while the list is open (see show()); if the editor
		// was ripped out of the DOM without closing the list first - htmx
		// swapping it out from under us - those listeners would otherwise
		// hang around forever pointing at a detached node. The
		// htmx:beforeSwap/htmx:beforeCleanupElement handlers below close the
		// list proactively, so this is a backstop for whatever they miss.
		function place() {
			if (!editor.isConnected) {
				window.removeEventListener('resize', place);
				window.removeEventListener('scroll', place, true);
				return;
			}
			if (!popover || list.hidden) { return; }
			var r = editor.getBoundingClientRect();
			var h = list.offsetHeight;
			var top = r.bottom + 4;
			if (top + h > window.innerHeight && r.top - 4 - h >= 0) { top = r.top - 4 - h; }
			list.style.top = top + 'px';
			list.style.left = r.left + 'px';
			list.style.maxWidth = r.width + 'px';
		}

		function show(on) {
			list.hidden = !on;
			if (!popover) { return; }
			var open = list.matches(':popover-open');
			if (on && !open) { list.showPopover(); } else if (!on && open) { list.hidePopover(); }
			// Follow the field while the list is open.
			var listen = on ? window.addEventListener : window.removeEventListener;
			listen.call(window, 'resize', place);
			listen.call(window, 'scroll', place, true);
			if (on) { place(); }
		}

		function close() {
			matches = [];
			active = -1;
			show(false);
			list.textContent = '';
			box.setAttribute('aria-expanded', 'false');
			box.removeAttribute('aria-activedescendant');
		}

		// suggest lists known tags that start with the typed text, then those
		// that contain it, leaving out the ones already on.
		function suggest() {
			var q = box.value.trim().toLowerCase();
			if (q === '') { close(); return; }
			var starts = [], contains = [];
			Object.keys(known).sort().forEach(function (k) {
				var t = known[k];
				if (has(t.name)) { return; }
				var at = k.indexOf(q);
				if (at === 0) { starts.push(t); } else if (at > 0) { contains.push(t); }
			});
			matches = starts.concat(contains).slice(0, MAX_SUGGEST);
			active = -1;
			list.textContent = '';
			matches.forEach(function (t, i) {
				var li = document.createElement('li');
				li.id = listId + '-' + i;
				li.className = 'tag-editor-option';
				li.setAttribute('role', 'option');
				li.setAttribute('aria-selected', 'false');
				var chip = document.createElement('span');
				chip.className = 'tag tag-' + colorOf(t.name);
				chip.title = t.name;
				chip.textContent = t.name;
				li.appendChild(chip);
				// mousedown would blur the box, which adds the typed text.
				li.addEventListener('mousedown', function (e) { e.preventDefault(); });
				li.addEventListener('click', function () { pick(i); box.focus(); });
				list.appendChild(li);
			});
			show(matches.length > 0);
			box.setAttribute('aria-expanded', matches.length ? 'true' : 'false');
			box.removeAttribute('aria-activedescendant');
		}

		function move(by) {
			if (!matches.length) { return; }
			active = active < 0 ? (by > 0 ? 0 : matches.length - 1) : (active + by + matches.length) % matches.length;
			Array.prototype.forEach.call(list.children, function (li, i) {
				li.setAttribute('aria-selected', i === active ? 'true' : 'false');
				li.classList.toggle('is-active', i === active);
			});
			var li = list.children[active];
			box.setAttribute('aria-activedescendant', li.id);
			li.scrollIntoView({ block: 'nearest' });
		}

		function pick(i) {
			var t = matches[i];
			box.value = '';
			close();
			if (t) { add(t.name); }
		}

		box.addEventListener('keydown', function (e) {
			switch (e.key) {
			case 'ArrowDown':
			case 'ArrowUp':
				e.preventDefault();
				if (list.hidden) { suggest(); }
				move(e.key === 'ArrowDown' ? 1 : -1);
				break;
			case 'Enter':
				// An empty box lets Enter submit the form as usual.
				if (active >= 0) { e.preventDefault(); pick(active); } else if (box.value.trim() !== '') { e.preventDefault(); commitBox(); }
				break;
			case ',':
				e.preventDefault();
				commitBox();
				break;
			case 'Tab':
				if (active >= 0) { e.preventDefault(); pick(active); } else if (box.value.trim() !== '') { e.preventDefault(); commitBox(); }
				break;
			case 'Escape':
				// Close the suggestions, not the dialog around the form.
				if (!list.hidden) { e.preventDefault(); e.stopPropagation(); close(); }
				break;
			case 'Backspace':
				if (box.value === '' && chips.length) { e.preventDefault(); remove(chips.length - 1); }
				break;
			}
		});
		box.addEventListener('input', suggest);
		box.addEventListener('blur', function () { if (box.value.trim() !== '') { commitBox(); } else { close(); } });
		box.addEventListener('paste', function (e) {
			var text = e.clipboardData && e.clipboardData.getData('text');
			if (!text || text.indexOf(',') < 0) { return; }
			e.preventDefault();
			add(box.value + text);
			box.value = '';
			close();
		});
		// A click on the field's empty space goes to the text box.
		editor.addEventListener('click', function (e) {
			if (e.target === editor) { box.focus(); }
		});

		input.value.split(',').forEach(function (part) {
			var name = clean(part);
			if (name !== '' && !has(name)) { chips.push(name); }
		});
		render();

		input._tagEditor = { add: add };
		// Exposed so htmx:beforeSwap/htmx:beforeCleanupElement (below) can
		// close the list - and so drop its window listeners - before the
		// editor is removed from the DOM.
		editor._tagEditorClose = close;
	}

	function initAll(root) {
		(root || document).querySelectorAll('input[data-tag-editor]').forEach(init);
	}

	// closeEditorsIn closes the suggestion list of every tag editor at or
	// within root, so none is left with window resize/scroll listeners once
	// htmx removes it.
	function closeEditorsIn(root) {
		if (!root || !root.querySelectorAll) { return; }
		var editors = root.classList && root.classList.contains('tag-editor') ? [root] : [];
		editors = editors.concat(Array.prototype.slice.call(root.querySelectorAll('.tag-editor')));
		editors.forEach(function (el) { if (el._tagEditorClose) { el._tagEditorClose(); } });
	}

	window.netisTags = {
		// add puts name on the editor of inputEl and reports whether that
		// input has one; dialog.js falls back to editing the text otherwise.
		add: function (inputEl, name) {
			if (!inputEl || !inputEl._tagEditor) { return false; }
			inputEl._tagEditor.add(name);
			return true;
		}
	};

	if (document.readyState === 'loading') {
		document.addEventListener('DOMContentLoaded', function () { initAll(); });
	} else {
		initAll();
	}
	document.addEventListener('htmx:afterSwap', function () { initAll(); });
	document.addEventListener('htmx:historyRestore', function () { initAll(); });
	// Close any editor's suggestion list before htmx removes it, so its
	// window resize/scroll listeners (added in show(), while the list is
	// open) get torn down instead of leaking.
	document.addEventListener('htmx:beforeSwap', function (e) { closeEditorsIn(e.detail && e.detail.target); });
	document.addEventListener('htmx:beforeCleanupElement', function (e) { closeEditorsIn(e.target); });
})();
