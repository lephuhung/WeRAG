"""Pure-stdlib .docx layout extraction.

Opens word/document.xml, word/styles.xml and word/theme/theme1.xml from the
OOXML package and resolves the effective paragraph/run properties needed for
format ("thể thức") checks: alignment, font, size, emphasis, indentation,
spacing, tab stops, table-column zone and section page setup.

No third-party dependencies: the module only uses zipfile + ElementTree so it
can run inside a bare Python sandbox.
"""

from __future__ import annotations

import io
import re
import zipfile
from dataclasses import dataclass, field, asdict
from typing import Optional
from xml.etree import ElementTree as ET

W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main"


def qn(local: str) -> str:
    return "{%s}%s" % (W_NS, local)


TWIPS_PER_MM = 1440.0 / 25.4


def twips_to_mm(value: Optional[str]) -> Optional[float]:
    if value is None:
        return None
    try:
        return round(int(value) / TWIPS_PER_MM, 2)
    except (TypeError, ValueError):
        return None


def half_points(value: Optional[str]) -> Optional[float]:
    if value is None:
        return None
    try:
        return int(value) / 2.0
    except (TypeError, ValueError):
        return None


def _bool_prop(el: Optional[ET.Element], default: bool = False) -> bool:
    """w:b/w:i/... are tri-state: absent → inherit(default), w:val=0/false → off."""
    if el is None:
        return default
    val = el.get(qn("val"))
    if val is None:
        return True
    return val not in ("0", "false", "off")


def _text_of_para(p: ET.Element, include_tabs: bool = True) -> str:
    parts = []
    for node in p.iter():
        tag = node.tag
        if tag == qn("t"):
            parts.append(node.text or "")
        elif tag == qn("tab") and include_tabs:
            parts.append("\t")
        elif tag == qn("br"):
            parts.append("\n")
        elif tag == qn("delText"):
            continue  # tracked-deletion text is not part of the document
    return "".join(parts)


def _rpr_props(rpr: Optional[ET.Element]) -> dict:
    out: dict = {}
    if rpr is None:
        return out
    fonts = rpr.find(qn("rFonts"))
    if fonts is not None:
        for key in ("ascii", "hAnsi", "eastAsia", "cs"):
            v = fonts.get(qn(key))
            if v:
                out.setdefault("font_" + key, v)
        for key in ("asciiTheme", "hAnsiTheme", "eastAsiaTheme", "cstheme"):
            v = fonts.get(qn(key))
            if v:
                out.setdefault("font_" + key, v)
    sz_el = rpr.find(qn("sz"))
    if sz_el is not None:
        v = half_points(sz_el.get(qn("val")))
        if v is not None:
            out["size_pt"] = v
    szcs = rpr.find(qn("szCs"))
    if szcs is not None:
        v = half_points(szcs.get(qn("val")))
        if v is not None:
            out["size_cs_pt"] = v
    if rpr.find(qn("b")) is not None:
        out["bold"] = _bool_prop(rpr.find(qn("b")))
    if rpr.find(qn("bCs")) is not None:
        out["bold_cs"] = _bool_prop(rpr.find(qn("bCs")))
    if rpr.find(qn("i")) is not None:
        out["italic"] = _bool_prop(rpr.find(qn("i")))
    u = rpr.find(qn("u"))
    if u is not None:
        out["underline"] = u.get(qn("val")) != "none"
    if rpr.find(qn("caps")) is not None:
        out["caps"] = _bool_prop(rpr.find(qn("caps")))
    if rpr.find(qn("smallCaps")) is not None:
        out["small_caps"] = _bool_prop(rpr.find(qn("smallCaps")))
    if rpr.find(qn("strike")) is not None:
        out["strike"] = _bool_prop(rpr.find(qn("strike")))
    va = rpr.find(qn("vertAlign"))
    if va is not None and va.get(qn("val")):
        out["vert_align"] = va.get(qn("val"))
    if rpr.find(qn("vanish")) is not None:
        out["hidden"] = _bool_prop(rpr.find(qn("vanish")))
    color = rpr.find(qn("color"))
    if color is not None and color.get(qn("val")):
        out["color"] = color.get(qn("val"))
    lang = rpr.find(qn("lang"))
    if lang is not None and lang.get(qn("val")):
        out["lang"] = lang.get(qn("val"))
    return out


def _ppr_props(ppr: Optional[ET.Element]) -> dict:
    out: dict = {}
    if ppr is None:
        return out
    jc = ppr.find(qn("jc"))
    if jc is not None and jc.get(qn("val")):
        val = jc.get(qn("val"))
        out["alignment"] = {"both": "justify", "distribute": "distribute"}.get(val, val)
    ind = ppr.find(qn("ind"))
    if ind is not None:
        for attr, key in (
            ("left", "indent_left_mm"), ("start", "indent_left_mm"),
            ("right", "indent_right_mm"), ("end", "indent_right_mm"),
            ("firstLine", "indent_first_line_mm"), ("hanging", "indent_hanging_mm"),
        ):
            mm = twips_to_mm(ind.get(qn(attr)))
            if mm is not None:
                out.setdefault(key, mm)
    spacing = ppr.find(qn("spacing"))
    if spacing is not None:
        for attr, key in (("before", "space_before_pt"), ("after", "space_after_pt")):
            v = spacing.get(qn(attr))
            if v is not None:
                try:
                    out.setdefault(key, int(v) / 20.0)
                except ValueError:
                    pass
        line = spacing.get(qn("line"))
        rule = spacing.get(qn("lineRule"))
        if line is not None:
            try:
                lv = int(line)
                if rule in ("exact", "atLeast"):
                    out["line_spacing_pt"] = lv / 20.0
                    out["line_spacing_rule"] = rule
                else:
                    out["line_spacing_multiple"] = round(lv / 240.0, 2)
            except ValueError:
                pass
    tabs = ppr.find(qn("tabs"))
    if tabs is not None:
        out["tab_stops"] = [
            {"pos_mm": twips_to_mm(t.get(qn("pos"))), "val": t.get(qn("val"))}
            for t in tabs.findall(qn("tab"))
        ]
    numpr = ppr.find(qn("numPr"))
    if numpr is not None:
        numid = numpr.find(qn("numId"))
        ilvl = numpr.find(qn("ilvl"))
        if numid is not None and numid.get(qn("val")):
            out["num_id"] = numid.get(qn("val"))
        if ilvl is not None and ilvl.get(qn("val")):
            out["ilvl"] = ilvl.get(qn("val"))
    if ppr.find(qn("pageBreakBefore")) is not None:
        out["page_break_before"] = _bool_prop(ppr.find(qn("pageBreakBefore")))
    pstyle = ppr.find(qn("pStyle"))
    if pstyle is not None and pstyle.get(qn("val")):
        out["style_id"] = pstyle.get(qn("val"))
    return out


@dataclass
class Run:
    text: str
    font_name: Optional[str] = None
    size_pt: Optional[float] = None
    bold: Optional[bool] = None
    italic: Optional[bool] = None
    underline: Optional[bool] = None
    caps: Optional[bool] = None


@dataclass
class Para:
    index: int
    text: str
    source: str = "body"  # body | table_cell | textbox | header | footer
    style_id: Optional[str] = None
    style_name: Optional[str] = None
    alignment: Optional[str] = None
    font_name: Optional[str] = None
    size_pt: Optional[float] = None
    bold: Optional[bool] = None
    italic: Optional[bool] = None
    underline: Optional[bool] = None
    caps: Optional[bool] = None
    text_is_upper: bool = False
    indent_left_mm: Optional[float] = None
    indent_right_mm: Optional[float] = None
    indent_first_line_mm: Optional[float] = None
    indent_hanging_mm: Optional[float] = None
    space_before_pt: Optional[float] = None
    space_after_pt: Optional[float] = None
    line_spacing_pt: Optional[float] = None
    line_spacing_multiple: Optional[float] = None
    num_id: Optional[str] = None
    ilvl: Optional[str] = None
    page_break_before: bool = False
    in_table: bool = False
    zone: str = "full"  # left | right | full | split
    left_text: str = ""
    right_text: str = ""
    # run formatting of each side of a split paragraph (the paragraph-level
    # props come from its longest run, which may sit on the other side)
    left_props: dict = field(default_factory=dict)
    right_props: dict = field(default_factory=dict)
    cell_left_mm: Optional[float] = None
    cell_right_mm: Optional[float] = None
    table_index: Optional[int] = None
    table_layout: Optional[str] = None  # columns | data (tables only)
    row: Optional[int] = None
    col: Optional[int] = None
    section: int = 0
    runs: list = field(default_factory=list)


@dataclass
class Section:
    index: int
    page_width_mm: Optional[float] = None
    page_height_mm: Optional[float] = None
    orientation: Optional[str] = None
    margin_top_mm: Optional[float] = None
    margin_right_mm: Optional[float] = None
    margin_bottom_mm: Optional[float] = None
    margin_left_mm: Optional[float] = None
    header_mm: Optional[float] = None
    footer_mm: Optional[float] = None
    gutter_mm: Optional[float] = None


@dataclass
class DocLayout:
    sections: list = field(default_factory=list)
    paragraphs: list = field(default_factory=list)  # flat, document order
    n_tables: int = 0
    has_headers: bool = False
    has_footers: bool = False
    errors: list = field(default_factory=list)

    def to_dict(self) -> dict:
        d = asdict(self)
        return d


# --------------------------------------------------------------------------
# Styles


def _parse_styles(xml_bytes: bytes) -> tuple:
    """Return (doc_defaults_rpr, doc_defaults_ppr, styles_by_id)."""
    doc_rpr: dict = {}
    doc_ppr: dict = {}
    styles: dict = {}
    try:
        root = ET.fromstring(xml_bytes)
    except ET.ParseError:
        return doc_rpr, doc_ppr, styles
    dd = root.find(qn("docDefaults"))
    if dd is not None:
        rprd = dd.find(qn("rPrDefault"))
        if rprd is not None:
            doc_rpr = _rpr_props(rprd.find(qn("rPr")))
        pprd = dd.find(qn("pPrDefault"))
        if pprd is not None:
            doc_ppr = _ppr_props(pprd.find(qn("pPr")))
    for st in root.findall(qn("style")):
        sid = st.get(qn("styleId"))
        if not sid:
            continue
        name_el = st.find(qn("name"))
        based = st.find(qn("basedOn"))
        styles[sid] = {
            "type": st.get(qn("type")),
            "name": name_el.get(qn("val")) if name_el is not None else sid,
            "based_on": based.get(qn("val")) if based is not None else None,
            "ppr": _ppr_props(st.find(qn("pPr"))),
            "rpr": _rpr_props(st.find(qn("rPr"))),
        }
    return doc_rpr, doc_ppr, styles


def _resolve_style(styles: dict, style_id: Optional[str]) -> tuple:
    """Merge pPr/rPr along the basedOn chain (ancestors first)."""
    ppr: dict = {}
    rpr: dict = {}
    name = None
    chain, seen, cur = [], set(), style_id
    while cur and cur in styles and cur not in seen:
        seen.add(cur)
        chain.append(cur)
        cur = styles[cur].get("based_on")
    for sid in reversed(chain):
        st = styles[sid]
        if name is None:
            name = st["name"]
        ppr.update({k: v for k, v in st["ppr"].items()})
        rpr.update({k: v for k, v in st["rpr"].items()})
    if chain:
        name = styles[chain[0]]["name"]
    return ppr, rpr, name


def _theme_fonts(zf: zipfile.ZipFile) -> dict:
    """Map theme tokens (majorHAnsi/minorHAnsi/...) to latin typefaces."""
    out = {}
    try:
        root = ET.fromstring(zf.read("word/theme/theme1.xml"))
    except (KeyError, ET.ParseError):
        return out
    scheme = root.find(".//{%s}fontScheme" % A_NS)
    if scheme is None:
        return out
    for which, key in (("majorFont", "major"), ("minorFont", "minor")):
        el = scheme.find("{%s}%s" % (A_NS, which))
        if el is None:
            continue
        latin = el.find("{%s}latin" % A_NS)
        if latin is not None and latin.get("typeface"):
            out[key] = latin.get("typeface")
    return out


_THEME_RE = re.compile(r"^(major|minor)(HAnsi|Ascii|Bidi|EastAsia)$", re.I)


def _effective_font(rpr: dict, theme: dict) -> Optional[str]:
    """Pick the latin font: explicit ascii/hAnsi first, else theme token."""
    for key in ("font_ascii", "font_hAnsi"):
        v = rpr.get(key)
        if v:
            return v
    for key in ("font_asciiTheme", "font_hAnsiTheme"):
        v = rpr.get(key)
        if v:
            m = _THEME_RE.match(v)
            if m:
                return theme.get(m.group(1).lower(), "theme:" + v)
            return "theme:" + v
    return rpr.get("font_eastAsia")


# --------------------------------------------------------------------------
# Document parse


def _parse_section(sect_pr: ET.Element, index: int) -> Section:
    sec = Section(index=index)
    pgsz = sect_pr.find(qn("pgSz"))
    if pgsz is not None:
        sec.page_width_mm = twips_to_mm(pgsz.get(qn("w")))
        sec.page_height_mm = twips_to_mm(pgsz.get(qn("h")))
        sec.orientation = pgsz.get(qn("orient"))
        if sec.orientation is None and sec.page_width_mm and sec.page_height_mm:
            sec.orientation = "landscape" if sec.page_width_mm > sec.page_height_mm else "portrait"
    pgmar = sect_pr.find(qn("pgMar"))
    if pgmar is not None:
        for attr, key in (
            ("top", "margin_top_mm"), ("right", "margin_right_mm"),
            ("bottom", "margin_bottom_mm"), ("left", "margin_left_mm"),
            ("header", "header_mm"), ("footer", "footer_mm"),
            ("gutter", "gutter_mm"),
        ):
            setattr(sec, key, twips_to_mm(pgmar.get(qn(attr))))
    return sec


def _merge(base: dict, overlay: dict) -> dict:
    out = dict(base)
    out.update({k: v for k, v in overlay.items() if v is not None})
    return out


class _Builder:
    def __init__(self, root: ET.Element, doc_rpr: dict, doc_ppr: dict,
                 styles: dict, theme: dict):
        self.root = root
        self.doc_rpr = doc_rpr
        self.doc_ppr = doc_ppr
        self.styles = styles
        self.theme = theme
        self.paras: list = []
        self.sections: list = []
        self.section_idx = 0
        self.n_tables = 0
        self.errors: list = []
        # every sectPr in document order (inline ones in pPr, then the body's
        # final one) — the final sectPr is only reached after all paragraphs,
        # so pre-scan to know page width/margins while deriving zones.
        self._all_sections: list = []
        self._table_layout_stack: list = []
        body = root.find(qn("body"))
        if body is not None:
            for sp in body.iter(qn("sectPr")):
                self._all_sections.append(_parse_section(sp, len(self._all_sections)))

    # -- effective props -------------------------------------------------

    def _para_defaults(self, ppr_props: dict):
        """Merge docDefaults + style chain + direct paragraph props."""
        style_id = ppr_props.get("style_id")
        st_ppr, st_rpr, st_name = _resolve_style(self.styles, style_id)
        eff_ppr = _merge(_merge(self.doc_ppr, st_ppr), ppr_props)
        eff_rpr = _merge(self.doc_rpr, st_rpr)
        return eff_ppr, eff_rpr, st_name

    def _run_rpr(self, para_rpr: dict, run_el: ET.Element) -> dict:
        rpr_el = run_el.find(qn("rPr"))
        own = _rpr_props(rpr_el) if rpr_el is not None else {}
        return _merge(para_rpr, own)

    # -- paragraphs -------------------------------------------------------

    def add_paragraph(self, p: ET.Element, source: str = "body", cell=None,
                      table_index=None, row=None, col=None) -> Para:
        ppr_el = p.find(qn("pPr"))
        ppr_props = _ppr_props(ppr_el)
        eff_ppr, para_rpr, style_name = self._para_defaults(ppr_props)

        # inline sectPr ends the current section
        sect_el = ppr_el.find(qn("sectPr")) if ppr_el is not None else None
        para_section = self.section_idx
        if sect_el is not None:
            self.sections.append(_parse_section(sect_el, self.section_idx))
            self.section_idx += 1

        runs: list = []
        for r in p.findall(qn("r")):
            txt_parts = []
            for node in r.iter():
                if node.tag == qn("t"):
                    txt_parts.append(node.text or "")
                elif node.tag == qn("tab"):
                    txt_parts.append("\t")
                elif node.tag == qn("br"):
                    txt_parts.append("\n")
            rrpr = self._run_rpr(para_rpr, r)
            runs.append(Run(
                text="".join(txt_parts),
                font_name=_effective_font(rrpr, self.theme),
                size_pt=rrpr.get("size_pt"),
                bold=rrpr.get("bold"),
                italic=rrpr.get("italic"),
                underline=rrpr.get("underline"),
                caps=rrpr.get("caps"),
            ))
        # runs nested inside hyperlinks / smartTags / sdt (direct children were
        # already gathered above — capture the nested ones' text too)
        direct_ids = {id(r) for r in p.findall(qn("r"))}
        for r in p.iter(qn("r")):
            if id(r) in direct_ids:
                continue
            rrpr = self._run_rpr(para_rpr, r)
            txt = "".join(t.text or "" for t in r.iter(qn("t")))
            if txt:
                runs.append(Run(text=txt,
                                font_name=_effective_font(rrpr, self.theme),
                                size_pt=rrpr.get("size_pt"),
                                bold=rrpr.get("bold"), italic=rrpr.get("italic"),
                                underline=rrpr.get("underline"),
                                caps=rrpr.get("caps")))

        text = "".join(r.text for r in runs)
        primary = self._primary_run(runs)

        para = Para(
            index=len(self.paras),
            text=text,
            source=source,
            style_id=ppr_props.get("style_id"),
            style_name=style_name,
            alignment=eff_ppr.get("alignment") or "left",
            font_name=primary.font_name if primary else _effective_font(para_rpr, self.theme),
            size_pt=primary.size_pt if primary else para_rpr.get("size_pt"),
            bold=primary.bold if primary else None,
            italic=primary.italic if primary else None,
            underline=primary.underline if primary else None,
            caps=primary.caps if primary else None,
            text_is_upper=_is_upper_text(text),
            indent_left_mm=eff_ppr.get("indent_left_mm"),
            indent_right_mm=eff_ppr.get("indent_right_mm"),
            indent_first_line_mm=eff_ppr.get("indent_first_line_mm"),
            indent_hanging_mm=eff_ppr.get("indent_hanging_mm"),
            space_before_pt=eff_ppr.get("space_before_pt"),
            space_after_pt=eff_ppr.get("space_after_pt"),
            line_spacing_pt=eff_ppr.get("line_spacing_pt"),
            line_spacing_multiple=eff_ppr.get("line_spacing_multiple"),
            num_id=eff_ppr.get("num_id"),
            ilvl=eff_ppr.get("ilvl"),
            page_break_before=bool(eff_ppr.get("page_break_before")),
            section=para_section,
            runs=[asdict(r) for r in runs],
        )
        if cell is not None:
            para.in_table = True
        para.table_index = table_index
        if cell is not None:
            para.table_layout = self._table_layout_stack[-1] if self._table_layout_stack else "columns"
        para.row = row
        para.col = col
        if cell:
            para.cell_left_mm, para.cell_right_mm = cell
        if source == "textbox":
            para.text_is_upper = _is_upper_text(text)
        self._derive_zone(para, eff_ppr)
        self.paras.append(para)
        return para

    @staticmethod
    def _primary_run(runs: list) -> Optional[Run]:
        """The run carrying the most visible text (skips hidden/empty)."""
        best, best_len = None, 0
        for r in runs:
            n = len(r.text.strip())
            if n > best_len:
                best, best_len = r, n
        return best

    def _derive_zone(self, para: Para, eff_ppr: dict):
        """Assign a horizontal zone: left / right / full / split (tab layout)."""
        left_edge, content_w = self._content_box()
        if para.table_index is not None and para.cell_left_mm is not None:
            if para.table_layout != "columns":
                # data table (bảng số liệu) or a single full-width cell — its
                # cells are body content, not a two-column header/footer
                para.zone = "full"
                return
            mid = content_w / 2.0
            cell_center = (
                para.cell_left_mm + (para.cell_right_mm or para.cell_left_mm)
            ) / 2.0
            para.zone = "left" if cell_center < mid else "right"
            return
        tabs = eff_ppr.get("tab_stops") or []
        if "\t" in para.text:
            rightish = [t for t in tabs
                        if t.get("pos_mm") and t["pos_mm"] >= content_w * 0.4]
            left, _, right = para.text.partition("\t")
            if (rightish or not tabs) and left.strip() and right.strip():
                para.zone = "split"
                para.left_text = left.strip()
                para.right_text = right.rsplit("\t", 1)[-1].strip() if "\t" in right else right.strip()
                para.left_props, para.right_props = _side_props(para.runs)
                return
            if not left.strip() and right.strip():
                # leading tabs push the text right ("\t\t\tCHỦ TỊCH"):
                # estimate where the text starts from the tab stops used
                n_tabs = len(para.text) - len(para.text.lstrip("\t "))
                n_tabs = para.text[:n_tabs].count("\t")
                stops = sorted(t["pos_mm"] for t in tabs
                               if t.get("pos_mm") and t.get("val") != "clear")
                x = (stops[n_tabs - 1] if len(stops) >= n_tabs
                     else n_tabs * 12.7)
                x += para.indent_left_mm or 0.0
                if x >= content_w * 0.4:
                    para.zone = "right"
                    return
        # a block pushed into the right half by a large left indent (khối ký
        # trình bày bằng thụt lề thay vì bảng)
        ind = para.indent_left_mm or 0.0
        if ind >= content_w * 0.4 and para.alignment != "right":
            para.zone = "right"
            return
        para.zone = "full"

    def _current_section(self) -> Optional[Section]:
        if self._all_sections:
            return self._all_sections[min(self.section_idx,
                                          len(self._all_sections) - 1)]
        if self.sections:
            return self.sections[-1]
        return None

    def _content_box(self) -> tuple:
        """(left margin, content width) of the current section, in mm."""
        sec = self._current_section()
        if sec and sec.page_width_mm and sec.margin_left_mm is not None \
                and sec.margin_right_mm is not None:
            return sec.margin_left_mm, (
                sec.page_width_mm - sec.margin_left_mm - sec.margin_right_mm)
        return 30.0, 160.0

    # -- tables -------------------------------------------------------------

    def add_table(self, tbl: ET.Element):
        ti = self.n_tables
        self.n_tables += 1
        grid = [twips_to_mm(gc.get(qn("w"))) or 0.0
                for gc in tbl.findall(qn("tblGrid") + "/" + qn("gridCol"))]
        rows = tbl.findall(qn("tr"))
        self._table_layout_stack.append(_classify_table(tbl, rows))
        try:
            for ri, tr in enumerate(rows):
                x = 0.0
                ci = 0
                for tc in tr.findall(qn("tc")):
                    span = 1
                    gs = tc.find(qn("tcPr") + "/" + qn("gridSpan"))
                    if gs is not None and gs.get(qn("val")):
                        try:
                            span = int(gs.get(qn("val")))
                        except ValueError:
                            span = 1
                    width = sum(grid[ci:ci + span]) if ci < len(grid) else 0.0
                    if not width:
                        # no/empty tblGrid: fall back to the cell's own width
                        tcw = tc.find(qn("tcPr") + "/" + qn("tcW"))
                        if tcw is not None and tcw.get(qn("type")) in (None, "dxa"):
                            width = twips_to_mm(tcw.get(qn("w"))) or 0.0
                    cell_extent = (x, x + (width or 0.0))
                    for child in _block_children(tc):
                        if child.tag == qn("p"):
                            self.add_paragraph(child, source="table_cell",
                                               cell=cell_extent, table_index=ti,
                                               row=ri, col=ci)
                            self._scan_textboxes(child, cell_extent, ti, ri, ci)
                        elif child.tag == qn("tbl"):
                            self.add_table(child)  # nested table: rare, keep order
                    x += width or 0.0
                    ci += span
        finally:
            self._table_layout_stack.pop()

    def _scan_textboxes(self, parent: ET.Element, cell, ti, ri, ci):
        for txbx in parent.iter(qn("txbxContent")):
            for p in txbx.findall(qn("p")):
                self.add_paragraph(p, source="textbox",
                                   cell=cell, table_index=ti, row=ri, col=ci)


_SIDE_KEYS = ("font_name", "size_pt", "bold", "italic", "underline", "caps")


def _side_props(runs: list) -> tuple:
    """Formatting of the dominant run left / right of the first tab."""
    best = [None, None]
    best_len = [0, 0]
    side = 0
    for r in runs:
        text = r.get("text", "")
        parts = text.split("\t")
        for k, part in enumerate(parts):
            if k > 0:
                side = 1
            n = len(part.strip())
            if n > best_len[side]:
                best[side], best_len[side] = r, n
    return tuple({k: b.get(k) for k in _SIDE_KEYS} if b else {} for b in best)


def _block_children(parent: ET.Element):
    """Block-level children (p / tbl / sectPr), unwrapping content controls
    (w:sdt/w:sdtContent) and w:customXml which templates use heavily."""
    for child in parent:
        if child.tag in (qn("sdt"), qn("customXml")):
            inner = child.find(qn("sdtContent")) if child.tag == qn("sdt") else child
            if inner is not None:
                yield from _block_children(inner)
        else:
            yield child


def _cell_has_text(tc: ET.Element) -> bool:
    return any((t.text or "").strip() for t in tc.iter(qn("t")))


def _table_has_borders(tbl: ET.Element) -> bool:
    tblpr = tbl.find(qn("tblPr"))
    if tblpr is None:
        return False
    borders = tblpr.find(qn("tblBorders"))
    if borders is not None:
        return any(b.get(qn("val")) not in (None, "nil", "none")
                   for b in borders)
    st = tblpr.find(qn("tblStyle"))
    return st is not None and "grid" in (st.get(qn("val")) or "").lower()


def _classify_table(tbl: ET.Element, rows: list) -> str:
    """'columns' = a borderless layout table placing text side by side (the
    two-column header / signature+nơi nhận block); 'data' = a real table
    (bảng số liệu) whose cells are body content."""
    max_text_cells = max_cells = 0
    for tr in rows:
        tcs = tr.findall(qn("tc"))
        max_cells = max(max_cells, len(tcs))
        max_text_cells = max(max_text_cells,
                             sum(1 for tc in tcs if _cell_has_text(tc)))
    # one column → full-width box; 3+ filled columns → a data grid.  A 3-col
    # header (cơ quan | spacer | quốc hiệu) still has only 2 filled cells.
    if max_cells < 2 or max_text_cells > 2:
        return "data"
    if _table_has_borders(tbl) and len(rows) >= 3:
        return "data"
    return "columns"


def _is_upper_text(text: str) -> bool:
    letters = [c for c in text if c.isalpha()]
    if len(letters) < 2:
        return False
    return all(not c.islower() for c in letters)


def inspect_docx(content: bytes) -> DocLayout:
    """Parse .docx bytes into a DocLayout."""
    layout = DocLayout()
    try:
        zf = zipfile.ZipFile(io.BytesIO(content))
    except zipfile.BadZipFile:
        layout.errors.append("not a .docx/.zip package")
        return layout
    theme = _theme_fonts(zf)
    doc_rpr: dict = {}
    doc_ppr: dict = {}
    styles: dict = {}
    try:
        doc_rpr, doc_ppr, styles = _parse_styles(zf.read("word/styles.xml"))
    except KeyError:
        pass
    try:
        doc_xml = zf.read("word/document.xml")
    except KeyError:
        layout.errors.append("word/document.xml missing")
        return layout
    try:
        root = ET.fromstring(doc_xml)
    except ET.ParseError as exc:
        layout.errors.append("document.xml parse error: %s" % exc)
        return layout

    builder = _Builder(root, doc_rpr, doc_ppr, styles, theme)
    body = root.find(qn("body"))
    if body is None:
        layout.errors.append("w:body missing")
        return layout

    for child in _block_children(body):
        if child.tag == qn("p"):
            builder.add_paragraph(child)
            # textboxes anchored in body paragraphs
            for txbx in child.iter(qn("txbxContent")):
                for p in txbx.findall(qn("p")):
                    builder.add_paragraph(p, source="textbox")
        elif child.tag == qn("tbl"):
            builder.add_table(child)
        elif child.tag == qn("sectPr"):
            builder.sections.append(_parse_section(child, builder.section_idx))
            builder.section_idx += 1

    layout.paragraphs = builder.paras
    layout.sections = builder.sections
    layout.n_tables = builder.n_tables
    layout.errors = builder.errors

    names = set(zf.namelist())
    layout.has_headers = any(re.match(r"word/header\d+\.xml$", n) for n in names)
    layout.has_footers = any(re.match(r"word/footer\d+\.xml$", n) for n in names)
    return layout
