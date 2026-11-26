#!/usr/bin/env python3
"""Native GTK3 (with tkinter fallback) GUI to add/remove VPN-routed apps for MyOVPN."""

import json
import os
import shlex
import sys
from pathlib import Path

FIELD_CODES = {"%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%i", "%c", "%k", "%v", "%m"}


def parse_desktop_file(path: str):
    name = ""
    exec_line = ""
    in_entry = False
    try:
        with open(path, "r", encoding="utf-8", errors="replace") as f:
            for raw in f:
                line = raw.strip()
                if not line or line.startswith("#"):
                    continue
                if line.startswith("[") and line.endswith("]"):
                    in_entry = line == "[Desktop Entry]"
                    continue
                if not in_entry or "=" not in line:
                    continue
                k, v = line.split("=", 1)
                k = k.strip()
                v = v.strip()
                if k == "Name" and not name:
                    name = v
                elif k == "Exec" and not exec_line:
                    exec_line = clean_exec(v)
    except OSError:
        return "", ""
    return name, exec_line


def clean_exec(raw: str) -> str:
    try:
        parts = shlex.split(raw)
    except ValueError:
        parts = raw.split()
    cleaned = [p for p in parts if p not in FIELD_CODES]
    return " ".join(shlex.quote(p) if " " in p else p for p in cleaned)


def format_cmd(command: str, args: list) -> str:
    parts = [command] + list(args or [])
    return " ".join(shlex.quote(p) if (" " in p or "\t" in p) else p for p in parts if p)


def run_gtk(payload: dict):
    import gi

    gi.require_version("Gtk", "3.0")
    from gi.repository import Gtk

    shortcuts = payload.get("shortcuts") or []
    current = [
        {
            "id": a.get("id", ""),
            "name": a.get("name", ""),
            "command_line": format_cmd(a.get("command", ""), a.get("args", [])),
        }
        for a in (payload.get("current_apps") or [])
    ]
    result = {"saved": False, "apps": current}

    win = Gtk.Window(title="MyOVPN — Manage VPN Apps")
    win.set_default_size(720, 560)
    win.set_border_width(12)
    win.set_position(Gtk.WindowPosition.CENTER)

    vbox = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=10)
    win.add(vbox)

    notebook = Gtk.Notebook()
    vbox.pack_start(notebook, True, True, 0)

    # --- Tab 1: Add App ---
    add_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
    add_box.set_border_width(10)

    lbl_pick = Gtk.Label(xalign=0)
    lbl_pick.set_markup("<b>1. Choose an installed app shortcut, or browse for an executable / .desktop file:</b>")
    add_box.pack_start(lbl_pick, False, False, 0)

    top_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
    search_entry = Gtk.SearchEntry()
    search_entry.set_placeholder_text("Filter installed apps (e.g. chromium, firefox, antigravity)…")
    top_row.pack_start(search_entry, True, True, 0)

    btn_browse = Gtk.Button(label="Browse File / Shortcut…")
    top_row.pack_start(btn_browse, False, False, 0)
    add_box.pack_start(top_row, False, False, 0)

    sc_store = Gtk.ListStore(str, str, str)  # name, command_line, desktop_file
    for sc in shortcuts:
        sc_store.append([sc.get("name", ""), sc.get("command_line", ""), sc.get("desktop_file", "")])

    def filter_fn(model, tree_iter, _data):
        q = search_entry.get_text().strip().lower()
        if not q:
            return True
        return q in (model[tree_iter][0] or "").lower() or q in (model[tree_iter][1] or "").lower()

    sc_filter = sc_store.filter_new()
    sc_filter.set_visible_func(filter_fn)

    sc_tree = Gtk.TreeView(model=sc_filter)
    sc_tree.append_column(Gtk.TreeViewColumn("App Name", Gtk.CellRendererText(), text=0))
    col_cmd = Gtk.TreeViewColumn("Command / Shortcut", Gtk.CellRendererText(), text=1)
    col_cmd.set_expand(True)
    sc_tree.append_column(col_cmd)

    sc_scroll = Gtk.ScrolledWindow()
    sc_scroll.set_policy(Gtk.PolicyType.AUTOMATIC, Gtk.PolicyType.AUTOMATIC)
    sc_scroll.set_min_content_height(200)
    sc_scroll.add(sc_tree)
    add_box.pack_start(sc_scroll, True, True, 0)

    lbl_edit = Gtk.Label(xalign=0)
    lbl_edit.set_markup("<b>2. App Name &amp; Command (you can paste a shortcut command or path directly):</b>")
    add_box.pack_start(lbl_edit, False, False, 0)

    grid = Gtk.Grid(column_spacing=8, row_spacing=6)
    grid.attach(Gtk.Label(label="Name:", xalign=0), 0, 0, 1, 1)
    name_entry = Gtk.Entry()
    name_entry.set_placeholder_text("e.g. Chromium (VPN) or Antigravity")
    name_entry.set_hexpand(True)
    grid.attach(name_entry, 1, 0, 1, 1)

    grid.attach(Gtk.Label(label="Command:", xalign=0), 0, 1, 1, 1)
    cmd_entry = Gtk.Entry()
    cmd_entry.set_placeholder_text("e.g. /usr/lib/chromium/chromium --user-data-dir=/home/owner/Applications/Chromium/profile-1")
    cmd_entry.set_hexpand(True)
    grid.attach(cmd_entry, 1, 1, 1, 1)
    add_box.pack_start(grid, False, False, 0)

    warn_lbl = Gtk.Label(xalign=0, wrap=True)
    warn_lbl.set_markup(
        "<small><i>Note: An app already running outside the VPN cannot be moved into it. "
        "Close existing windows of that browser profile first, or pass a separate --user-data-dir=.</i></small>"
    )
    add_box.pack_start(warn_lbl, False, False, 0)

    add_action_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
    status_lbl = Gtk.Label(xalign=0)
    add_action_row.pack_start(status_lbl, True, True, 0)
    btn_add = Gtk.Button(label="Add to VPN List")
    btn_add.get_style_context().add_class("suggested-action")
    add_action_row.pack_end(btn_add, False, False, 0)
    add_box.pack_start(add_action_row, False, False, 0)

    notebook.append_page(add_box, Gtk.Label(label="Add App"))

    # --- Tab 2: Current VPN Apps ---
    cur_box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=8)
    cur_box.set_border_width(10)

    cur_store = Gtk.ListStore(str, str, str)  # id, name, command_line
    for a in current:
        cur_store.append([a["id"], a["name"], a["command_line"]])

    cur_tree = Gtk.TreeView(model=cur_store)
    cur_tree.append_column(Gtk.TreeViewColumn("Name", Gtk.CellRendererText(), text=1))
    col_cur_cmd = Gtk.TreeViewColumn("Command", Gtk.CellRendererText(), text=2)
    col_cur_cmd.set_expand(True)
    cur_tree.append_column(col_cur_cmd)

    cur_scroll = Gtk.ScrolledWindow()
    cur_scroll.set_policy(Gtk.PolicyType.AUTOMATIC, Gtk.PolicyType.AUTOMATIC)
    cur_scroll.add(cur_tree)
    cur_box.pack_start(cur_scroll, True, True, 0)

    cur_btn_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
    btn_remove = Gtk.Button(label="Remove Selected")
    cur_btn_row.pack_start(btn_remove, False, False, 0)
    cur_box.pack_start(cur_btn_row, False, False, 0)

    cur_tab_lbl = Gtk.Label(label=f"VPN Apps ({len(current)})")
    notebook.append_page(cur_box, cur_tab_lbl)

    # --- Bottom Save / Cancel ---
    bottom_row = Gtk.Box(orientation=Gtk.Orientation.HORIZONTAL, spacing=8)
    btn_cancel = Gtk.Button(label="Cancel")
    btn_save = Gtk.Button(label="Save & Close")
    btn_save.get_style_context().add_class("suggested-action")
    bottom_row.pack_end(btn_save, False, False, 0)
    bottom_row.pack_end(btn_cancel, False, False, 0)
    vbox.pack_end(bottom_row, False, False, 0)

    # Handlers
    search_entry.connect("search-changed", lambda _e: sc_filter.refilter())

    def on_sc_select(selection):
        model, tree_iter = selection.get_selected()
        if tree_iter is not None:
            name_entry.set_text(model[tree_iter][0] or "")
            cmd_entry.set_text(model[tree_iter][1] or "")

    sc_tree.get_selection().connect("changed", on_sc_select)

    def on_browse(_btn):
        dlg = Gtk.FileChooserDialog(
            title="Select Application Executable or .desktop Shortcut",
            parent=win,
            action=Gtk.FileChooserAction.OPEN,
        )
        dlg.add_buttons(Gtk.STOCK_CANCEL, Gtk.ResponseType.CANCEL, Gtk.STOCK_OPEN, Gtk.ResponseType.OK)
        dlg.set_current_folder(os.path.expanduser("~"))

        flt_all = Gtk.FileFilter()
        flt_all.set_name("All files / executables / .desktop")
        flt_all.add_pattern("*")
        dlg.add_filter(flt_all)

        flt_desk = Gtk.FileFilter()
        flt_desk.set_name("Desktop shortcuts (*.desktop)")
        flt_desk.add_pattern("*.desktop")
        dlg.add_filter(flt_desk)

        if dlg.run() == Gtk.ResponseType.OK:
            chosen = dlg.get_filename()
            if chosen:
                if chosen.lower().endswith(".desktop"):
                    d_name, d_exec = parse_desktop_file(chosen)
                    if d_name:
                        name_entry.set_text(d_name)
                    if d_exec:
                        cmd_entry.set_text(d_exec)
                    else:
                        status_lbl.set_markup("<span foreground='#dc2626'>Could not read Exec= from .desktop file.</span>")
                else:
                    base = Path(chosen).stem.replace("-", " ").replace("_", " ").title()
                    if not name_entry.get_text().strip():
                        name_entry.set_text(base)
                    quoted = shlex.quote(chosen) if " " in chosen else chosen
                    cmd_entry.set_text(quoted)
        dlg.destroy()

    btn_browse.connect("clicked", on_browse)

    def on_add(_btn):
        nm = name_entry.get_text().strip()
        cm = cmd_entry.get_text().strip()
        if not cm:
            status_lbl.set_markup("<span foreground='#dc2626'>Enter or select a command/file first.</span>")
            return
        if not nm:
            nm = Path(shlex.split(cm)[0]).stem
            name_entry.set_text(nm)
        cur_store.append(["", nm, cm])
        cur_tab_lbl.set_text(f"VPN Apps ({len(cur_store)})")
        status_lbl.set_markup(f"<span foreground='#16a34a'>Added <b>{nm}</b>. Click Save &amp; Close when done.</span>")

    btn_add.connect("clicked", on_add)

    def on_remove(_btn):
        model, tree_iter = cur_tree.get_selection().get_selected()
        if tree_iter is not None:
            model.remove(tree_iter)
            cur_tab_lbl.set_text(f"VPN Apps ({len(cur_store)})")

    btn_remove.connect("clicked", on_remove)

    def on_save(_btn):
        result["saved"] = True
        result["apps"] = [
            {"id": row[0], "name": row[1], "command_line": row[2]}
            for row in cur_store
        ]
        Gtk.main_quit()

    btn_save.connect("clicked", on_save)
    btn_cancel.connect("clicked", lambda _b: Gtk.main_quit())
    win.connect("destroy", lambda _w: Gtk.main_quit())

    win.show_all()
    Gtk.main()
    return result


def run_tk(payload: dict):
    import tkinter as tk
    from tkinter import filedialog, messagebox, ttk

    shortcuts = payload.get("shortcuts") or []
    current = [
        {
            "id": a.get("id", ""),
            "name": a.get("name", ""),
            "command_line": format_cmd(a.get("command", ""), a.get("args", [])),
        }
        for a in (payload.get("current_apps") or [])
    ]
    result = {"saved": False, "apps": current}

    root = tk.Tk()
    root.title("MyOVPN — Manage VPN Apps")
    root.geometry("700x520")

    nb = ttk.Notebook(root)
    nb.pack(fill="both", expand=True, padx=10, pady=10)

    tab_add = ttk.Frame(nb, padding=10)
    tab_cur = ttk.Frame(nb, padding=10)
    nb.add(tab_add, text="Add App")
    nb.add(tab_cur, text=f"VPN Apps ({len(current)})")

    top = ttk.Frame(tab_add)
    top.pack(fill="x")
    search_var = tk.StringVar()
    ttk.Entry(top, textvariable=search_var).pack(side="left", fill="x", expand=True)

    lb = tk.Listbox(tab_add, height=10)
    lb.pack(fill="both", expand=True, pady=6)
    filtered = list(shortcuts)

    def refresh_lb(*_a):
        nonlocal filtered
        q = search_var.get().strip().lower()
        lb.delete(0, tk.END)
        filtered = [s for s in shortcuts if q in s.get("name", "").lower() or q in s.get("command_line", "").lower()]
        for s in filtered:
            lb.insert(tk.END, f"{s.get('name', '')}  —  {s.get('command_line', '')}")

    search_var.trace_add("write", refresh_lb)
    refresh_lb()

    name_var = tk.StringVar()
    cmd_var = tk.StringVar()

    def on_lb_select(_evt):
        sel = lb.curselection()
        if sel:
            s = filtered[sel[0]]
            name_var.set(s.get("name", ""))
            cmd_var.set(s.get("command_line", ""))

    lb.bind("<<ListboxSelect>>", on_lb_select)

    def browse():
        p = filedialog.askopenfilename(initialdir=os.path.expanduser("~"), title="Select Executable or .desktop Shortcut")
        if not p:
            return
        if p.lower().endswith(".desktop"):
            dn, de = parse_desktop_file(p)
            if dn:
                name_var.set(dn)
            if de:
                cmd_var.set(de)
        else:
            if not name_var.get().strip():
                name_var.set(Path(p).stem.title())
            cmd_var.set(shlex.quote(p) if " " in p else p)

    ttk.Button(top, text="Browse File…", command=browse).pack(side="right", padx=(6, 0))

    form = ttk.Frame(tab_add)
    form.pack(fill="x", pady=4)
    ttk.Label(form, text="Name:").grid(row=0, column=0, sticky="w")
    ttk.Entry(form, textvariable=name_var).grid(row=0, column=1, sticky="ew", padx=6)
    ttk.Label(form, text="Command:").grid(row=1, column=0, sticky="w", pady=(4, 0))
    ttk.Entry(form, textvariable=cmd_var).grid(row=1, column=1, sticky="ew", padx=6, pady=(4, 0))
    form.columnconfigure(1, weight=1)

    cur_lb = tk.Listbox(tab_cur)
    cur_lb.pack(fill="both", expand=True)
    for a in current:
        cur_lb.insert(tk.END, f"{a['name']}  —  {a['command_line']}")

    def add_app():
        nm = name_var.get().strip()
        cm = cmd_var.get().strip()
        if not cm:
            messagebox.showwarning("MyOVPN", "Enter or select a command first.")
            return
        if not nm:
            nm = Path(shlex.split(cm)[0]).stem
        current.append({"id": "", "name": nm, "command_line": cm})
        cur_lb.insert(tk.END, f"{nm}  —  {cm}")
        nb.tab(1, text=f"VPN Apps ({len(current)})")

    ttk.Button(tab_add, text="Add to VPN List", command=add_app).pack(anchor="e", pady=6)

    def remove_app():
        sel = cur_lb.curselection()
        if sel:
            idx = sel[0]
            cur_lb.delete(idx)
            del current[idx]
            nb.tab(1, text=f"VPN Apps ({len(current)})")

    ttk.Button(tab_cur, text="Remove Selected", command=remove_app).pack(anchor="w", pady=6)

    btns = ttk.Frame(root)
    btns.pack(fill="x", padx=10, pady=(0, 10))

    def save_close():
        result["saved"] = True
        result["apps"] = current
        root.destroy()

    ttk.Button(btns, text="Save & Close", command=save_close).pack(side="right", padx=(6, 0))
    ttk.Button(btns, text="Cancel", command=root.destroy).pack(side="right")

    root.mainloop()
    return result


def main():
    payload = json.load(sys.stdin)
    try:
        res = run_gtk(payload)
    except Exception:
        res = run_tk(payload)
    json.dump(res, sys.stdout)


if __name__ == "__main__":
    main()
