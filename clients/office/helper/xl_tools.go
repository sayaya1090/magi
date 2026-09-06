package office

var sheetProp = property{Name: "sheet", Type: "string", Topic: true, Desc: "Worksheet name exactly as the tab reads (\"2분기\", \"Sheet1\"). Omit it for the sheet the person is looking at right now — that is what \"this sheet\" means. A number is accepted too (1-based tab position) but names are safer: tabs get reordered.", Also: []string{"worksheet"}}
var addressProp = property{Name: "address", Type: "string", Desc: "A1-style range: a cell (\"C3\"), a block (\"B2:E9\"), whole columns (\"A:C\") or rows (\"5:7\"). Omit it where the description says so (usually: the sheet's used range). Never put the sheet name inside the address — that is what sheet is for.", Also: []string{"range"}}

func withSheet(rest ...property) []property {
	return append([]property{sheetProp}, rest...)
}
func withRange(rest ...property) []property {
	return append([]property{sheetProp, addressProp}, rest...)
}

// xlCatalogue 는 도구 목록. hasCouncil 은 읽기 도구 설명의 마무리 안내만 바꾼다(파워포인트 판과 같은
// 이유: magi 의 MCP 클라이언트는 핸드셰이크의 instructions 를 버려서 설명문이 유일한 자리다).
func xlCatalogue(hasCouncil bool) []tool {
	declare := ""
	if hasCouncil {
		declare = " A turn that called any tool must end by declaring it finished with " +
			"council{complete:true}, even a read-only one: otherwise the turn lands UNVERIFIED."
	}
	return []tool{
		// ── 읽기 ────────────────────────────────────────────────────────────────────
		{
			Name: "list_sheets",
			Desc: "A WORKBOOK IS ALREADY OPEN IN EXCEL AND THESE TOOLS ARE ATTACHED TO IT. You do not " +
				"create, open or upload a workbook, and there is no tool that does — the person is looking at " +
				"theirs right now and every tool here edits that one. Never ask them to provide, upload or " +
				"open a file; if a call fails, the workbook is still there and the call is what went wrong — " +
				"RETRY IT. Do not fall back to building a workbook any other way: an .xlsx you write with a " +
				"shell, openpyxl, pandas or COM automation is a FILE NOBODY IS LOOKING AT, and the person ends " +
				"up with an unchanged workbook on screen plus scripts they did not ask for. If these tools " +
				"truly cannot do it, say so and stop. " +
				"THE WORKBOOK'S TABLE OF CONTENTS: every worksheet with its 1-based tab position, name, visibility, " +
				"used range address and size, and how many tables, charts and pivot tables it holds. The row marked " +
				"active:true is the sheet the person is looking at. Read this first; it is one call and it tells " +
				"you where everything is." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "describe_sheet",
			Desc: "One worksheet as Excel models it: used range, frozen panes, merged areas, every table (name, address, " +
				"header names, row count), every chart (name, type, title, source), pivot tables, named items scoped " +
				"to it, and the header row's look. Call this before writing into a sheet you did not build — it says " +
				"where the data ends and what already sits on the sheet. Cheap: no cell values are returned; use " +
				"read_range for those." + declare,
			Props:    withSheet(),
			ReadOnly: true,
		},
		{
			Name: "read_range",
			Desc: "Cell values of a range as a 2-D array (rows of cells), plus formulas where a cell has one and the number " +
				"format when it is not General. Omit address for the sheet's used range. Big ranges are cut at " +
				"max_rows/max_cols and the answer says so — read the part you need, not the whole sheet twice. Dates " +
				"come back as Excel serial numbers with the number format beside them; text as text; empty cells as " +
				"\"\"." + declare,
			Props: withRange(
				property{Name: "max_rows", Type: "integer", Desc: "Cap on rows returned (default 200)."},
				property{Name: "max_cols", Type: "integer", Desc: "Cap on columns returned (default 30)."},
				property{Name: "formulas", Type: "boolean", Desc: "Include formulas (default true). false returns values only, which is smaller."},
			),
			ReadOnly: true,
		},
		{
			Name: "find",
			Desc: "Cells whose value or formula contains text — across one sheet or the whole workbook. Returns " +
				"sheet, address and the cell's value for each hit, capped at limit. The way into \"where is the " +
				"total\" and \"which cells reference Q3\". Needs ExcelApi 1.9; refused with a reason below that." + declare,
			Props: withSheet(
				property{Name: "text", Type: "string", Desc: "Text to look for. Required."},
				property{Name: "match_case", Type: "boolean", Desc: "Case-sensitive (default false)."},
				property{Name: "whole_cell", Type: "boolean", Desc: "Match the whole cell, not a substring (default false)."},
				property{Name: "in_formulas", Type: "boolean", Desc: "Search formulas instead of values (default false)."},
				property{Name: "limit", Type: "integer", Desc: "Maximum hits (default 50)."},
			),
			Required: []string{"text"},
			ReadOnly: true,
		},
		{
			Name: "read_table",
			Desc: "One table (a ListObject) by name: its address, header names and rows as arrays. rows beyond max_rows " +
				"are cut and the answer says so. Table names come from list_sheets / describe_sheet." + declare,
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name (\"Table1\", \"매출\"). Required."},
				{Name: "max_rows", Type: "integer", Desc: "Cap on rows returned (default 200)."},
			},
			Required: []string{"table"},
			ReadOnly: true,
		},
		{
			Name: "read_chart",
			Desc: "One chart by name: type, title, axis titles, legend, each series with its name and source range, " +
				"and where it sits (left/top/width/height in points). Chart names come from describe_sheet." + declare,
			Props: withSheet(
				property{Name: "chart", Type: "string", Desc: "Chart name (\"Chart 1\"). Required."},
			),
			Required: []string{"chart"},
			ReadOnly: true,
		},
		{
			Name: "render_range",
			Desc: "A PNG of a range as Excel draws it — formats, borders, conditional colours, wrapped text. **The most " +
				"expensive tool here**; only a vision model can see it. Use it to check a layout you built (does " +
				"the header fit, are the numbers aligned), not to read values — read_range is cheaper and exact. " +
				"Omit address for the used range. Needs ExcelApi 1.7." + declare,
			Props: withRange(
				property{Name: "max_width", Type: "integer", Desc: "Widest edge in pixels (default 1024, 160–4096)."},
			),
			ReadOnly: true,
		},
		{
			Name: "render_chart",
			Desc: "A PNG of one chart. Cheaper than render_range of the whole sheet when the question is about the " +
				"chart. Needs ExcelApi 1.2." + declare,
			Props: withSheet(
				property{Name: "chart", Type: "string", Desc: "Chart name. Required."},
				property{Name: "max_width", Type: "integer", Desc: "Width in pixels (default 800)."},
			),
			Required: []string{"chart"},
			ReadOnly: true,
		},
		{
			Name: "read_comments",
			Desc: "Threaded comments on a sheet (or the whole workbook when sheet is omitted): cell, author, text, " +
				"replies, resolved flag. Needs ExcelApi 1.10. On Excel 2021 for Windows, which has no threaded-comment API, the helper " +
				"reads cell NOTES instead and the answer says so (kind: note)." + declare,
			Props:    withSheet(),
			ReadOnly: true,
		},
		{
			Name: "read_names",
			Desc: "Named items (defined names) of the workbook and, when sheet is given, of that sheet: name, what it " +
				"refers to, its value when it is a constant. Formulas people wrote use these names — read them " +
				"before rewriting formulas." + declare,
			Props:    withSheet(),
			ReadOnly: true,
		},
		{
			Name: "read_validation",
			Desc: "Data validation rules on a range: kind (list, whole_number, …), the rule, prompt and error " +
				"messages. Empty when the range has none. Needs ExcelApi 1.8." + declare,
			Props:    withRange(),
			Required: []string{"address"},
			ReadOnly: true,
		},
		{
			Name: "read_conditional_formats",
			Desc: "Conditional formats that touch a range (omit address for the whole sheet): kind, rule, priority. " +
				"Read before adding one so you do not stack a second rule on the first. Needs ExcelApi 1.6." + declare,
			Props:    withRange(),
			ReadOnly: true,
		},
		{
			Name: "describe_style",
			Desc: "What this workbook's tables and headers consistently look like — header fill and font, body font " +
				"and size, number formats in use — and how many sheets that was measured over. Read it before " +
				"laying out a new sheet so the new one matches the old ones." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "snapshot_range",
			Desc: "Keep a copy of a range's values, formulas and number formats so restore_range can put them back. " +
				"Take one before a bulk write over cells that already hold something. Costs one call and no " +
				"approval; the snapshot lives in the task pane for this session." + declare,
			Props:    withRange(),
			Required: []string{"address"},
			ReadOnly: true,
		},
		{
			Name: "read_tags",
			Desc: "Notes you left in this workbook earlier (workbook settings, invisible to the person). Your memory " +
				"between conversations: which sheet you built, what the person asked for, which table is \"the " +
				"summary\". Read before rearranging a sheet you may have built." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "read_suggestions",
			Desc: "Fix-suggestions sitting in this workbook, from you or an earlier conversation. They live in the " +
				"file and show as cards in the task pane with an Apply button. Read before offering advice on a " +
				"workbook you did not just build." + declare,
			Props:    withSheet(),
			ReadOnly: true,
		},
		{
			Name: "advise",
			Desc: "Pin advice in the task pane without touching the workbook: what to change and why, optionally " +
				"pointing at a sheet and range. Clicking an item selects that range. Advice is what you would SAY, " +
				"not what you did — it never counts as work finished." + declare,
			Props: []property{
				{Name: "items", Type: "array", Items: "object", Desc: "[{message, why, sheet, address}] — message and why required; sheet/address optional."},
			},
			Required: []string{"items"},
			ReadOnly: true,
		},
		{
			Name:     "clear_advice",
			Desc:     "Take the pinned advice down. Nothing to undo: it was never in the workbook." + declare,
			Props:    []property{},
			ReadOnly: true,
		},

		// ── 셀 쓰기·서식 ──────────────────────────────────────────────────────────────
		{
			Name: "write_range",
			Desc: "Write values or formulas into a range. values is a 2-D array — rows of cells — and its shape must " +
				"match the address exactly (a 3×2 block into \"B2:C4\"); a single cell takes [[x]]. Give the " +
				"top-left cell alone (\"B2\") and the block is sized from the array. Formulas start with \"=\" and " +
				"go in formulas (or in values — a string starting with = is a formula either way). Cells that held " +
				"something are OVERWRITTEN and the answer says how many; snapshot_range first if that matters. " +
				"Numbers stay numbers: send 1234, not \"1234\". Dates: send an ISO string (\"2026-09-06\") and " +
				"set number_format, or a serial number.",
			Props: withRange(
				property{Name: "values", Type: "array", Items: "array", Desc: "Rows of cell values. Strings, numbers, booleans, or null for an empty cell."},
				property{Name: "formulas", Type: "array", Items: "array", Desc: "Rows of formulas (\"=SUM(B2:B9)\"). Same shape rule. Use either values or formulas, or both for different cells (null where the other applies)."},
				property{Name: "number_format", Type: "string", Desc: "Number format applied to the whole block, e.g. \"#,##0\", \"0.0%\", \"yyyy-mm-dd\", \"₩#,##0\"."},
			),
			Required: []string{"address"},
		},
		{
			Name: "replace_all",
			Desc: "Find and replace text in cell values across one sheet or the whole workbook. Answers how many cells " +
				"changed. Case-insensitive and substring by default. Needs ExcelApi 1.9.",
			Props: withSheet(
				property{Name: "find", Type: "string", Desc: "Text to replace. Required."},
				property{Name: "replace", Type: "string", Desc: "Replacement (empty string removes it). Required."},
				property{Name: "match_case", Type: "boolean", Desc: "Case-sensitive (default false)."},
				property{Name: "whole_cell", Type: "boolean", Desc: "Only cells whose whole content matches (default false)."},
			),
			Required: []string{"find", "replace"},
		},
		{
			Name: "copy_range",
			Desc: "Copy a block to another place — values, formulas, formats or all (default) — optionally transposed. " +
				"`address` is the destination's top-left cell; the source may be on another sheet (`source_sheet`). Needs ExcelApi 1.9.",
			Props: withRange(
				property{Name: "source", Type: "string", Desc: "Source A1 range, e.g. \"B2:E9\". Required."},
				property{Name: "source_sheet", Type: "string", Desc: "Sheet of the source when it differs from `sheet`."},
				property{Name: "mode", Type: "string", Desc: "all (default), values, formulas, formats.", Enum: xlCopyModes},
				property{Name: "transpose", Type: "boolean", Desc: "Swap rows and columns (default false)."},
			),
			Required: []string{"source", "address"},
		},
		{
			Name: "fill_range",
			Desc: "Fill down/right the way the fill handle does: extend `address` (the seed cells — a formula, a pattern, " +
				"a series start) over `to` (a range that INCLUDES the seed). `fill` picks Excel's behaviour: default (Excel " +
				"decides), copy, series, formats, values. Needs ExcelApi 1.9.",
			Props: withRange(
				property{Name: "to", Type: "string", Desc: "Destination range including the seed, e.g. seed C2 → to \"C2:C20\". Required."},
				property{Name: "fill", Type: "string", Desc: "default, copy, series, formats, values.", Enum: xlFillKinds},
			),
			Required: []string{"address", "to"},
		},
		{
			Name: "remove_duplicates",
			Desc: "Remove duplicate rows inside a block, judged by the given columns (0-based within the block; omit for all " +
				"columns). Answers how many rows went and how many remain. Needs ExcelApi 1.9.",
			Props: withRange(
				property{Name: "columns", Type: "array", Items: "integer", Desc: "Columns that define a duplicate, 0-based within the block."},
				property{Name: "has_header", Type: "boolean", Desc: "First row is a header and is kept (default true)."},
			),
			Required: []string{"address"},
		},
		{
			Name: "set_cell_style",
			Desc: "Apply one of Excel's built-in cell styles (Good, Bad, Neutral, Input, Heading1…, Total, Percent…) to a range — " +
				"the styles under Home › Cell Styles, language-independent names. Needs ExcelApi 1.7.",
			Props:    withRange(property{Name: "style", Type: "string", Desc: "Built-in style name. Required.", Enum: xlCellStyles}),
			Required: []string{"address", "style"},
		},
		{
			Name: "set_number_format",
			Desc: "Number format of a range: \"#,##0\", \"#,##0.00\", \"0%\", \"yyyy-mm-dd\", \"@\" (text), \"General\". " +
				"Changes how the values SHOW, not the values.",
			Props: withRange(
				property{Name: "format", Type: "string", Desc: "Excel number format code. Required.", Also: []string{"number_format"}},
			),
			Required: []string{"address", "format"},
		},
		{
			Name: "format_range",
			Desc: "The look of a range: font (name, size, bold, italic, colour), fill, alignment, wrap, borders, " +
				"column width and row height. Any subset — only what you give changes. Header rows are the usual " +
				"reason to call this. For a whole table's style use add_table's table_style instead.",
			Props: withRange(
				property{Name: "font", Type: "string", Desc: "Font family name."},
				property{Name: "size", Type: "number", Desc: "Font size in points."},
				property{Name: "bold", Type: "boolean"}, property{Name: "italic", Type: "boolean"},
				property{Name: "underline", Type: "boolean"},
				property{Name: "color", Type: "string", Desc: "Font colour as #RRGGBB."},
				property{Name: "fill", Type: "string", Desc: "Cell fill as #RRGGBB, or \"none\" to clear."},
				property{Name: "align", Type: "string", Desc: "Horizontal: General, Left, Center, Right, Fill, Justify, CenterAcrossSelection, Distributed.", Enum: xlAligns},
				property{Name: "valign", Type: "string", Desc: "Vertical: Top, Center, Bottom, Justify, Distributed.", Enum: xlValigns},
				property{Name: "wrap", Type: "boolean", Desc: "Wrap text inside the cell."},
				property{Name: "indent", Type: "integer", Desc: "Indent level 0–15."},
				property{Name: "borders", Type: "string", Desc: "Border colour as #RRGGBB for all four edges plus inner lines, or \"none\" to clear."},
				property{Name: "border_style", Type: "string", Desc: "Continuous (default), Dash, DashDot, DashDotDot, Dot, Double, SlantDashDot, None.", Enum: xlBorderStyles},
				property{Name: "column_width", Type: "number", Desc: "Width in points for every column in the range. Omit to leave; use autofit for a fit."},
				property{Name: "row_height", Type: "number", Desc: "Height in points for every row in the range."},
			),
			Required: []string{"address"},
		},
		{
			Name: "clear_range",
			Desc: "Clear a range — everything (default), or only contents, formats or hyperlinks. Cells stay; use " +
				"delete_cells to remove them.",
			Props: withRange(
				property{Name: "what", Type: "string", Desc: "all (default), contents, formats, hyperlinks.", Enum: xlClearWhats},
			),
			Required: []string{"address"},
		},
		{
			Name: "merge_cells",
			Desc: "Merge a range into one cell (the top-left value survives). Across the row only when across is true. " +
				"Merged title rows are the one good use; merging inside data breaks sorting and tables.",
			Props: withRange(
				property{Name: "across", Type: "boolean", Desc: "Merge each row separately (default false)."},
			),
			Required: []string{"address"},
		},
		{
			Name:     "unmerge_cells",
			Desc:     "Unmerge every merged cell inside a range.",
			Props:    withRange(),
			Required: []string{"address"},
		},
		{
			Name: "insert_cells",
			Desc: "Insert blank cells at a range, shifting the existing cells down or right. To insert whole rows " +
				"give a row address (\"5:7\"), whole columns a column address (\"C:D\").",
			Props: withRange(
				property{Name: "shift", Type: "string", Desc: "down (default) or right — where the existing cells go.", Enum: xlInsertShifts},
			),
			Required: []string{"address"},
		},
		{
			Name: "delete_cells",
			Desc: "Delete cells, shifting the rest up or left. Whole rows/columns with a row/column address. This " +
				"removes data and there is no undo but restore_range — snapshot first if the cells hold anything.",
			Props: withRange(
				property{Name: "shift", Type: "string", Desc: "up (default) or left — where the remaining cells come from.", Enum: xlDeleteShifts},
			),
			Required: []string{"address"},
		},
		{
			Name: "autofit",
			Desc: "Fit column widths and/or row heights to their contents. Call it after writing a block so " +
				"nothing shows as ####. Omit address for the used range.",
			Props: withRange(
				property{Name: "what", Type: "string", Desc: "columns (default), rows, or both.", Enum: xlAutofitWhats},
			),
		},
		{
			Name: "set_hyperlink",
			Desc: "Put a hyperlink on a cell: a web address, or a place in this workbook (sheet + address). An empty " +
				"url with no target removes the link. Needs ExcelApi 1.7.",
			Props: withRange(
				property{Name: "url", Type: "string", Desc: "https://… or mailto:…; empty to remove."},
				property{Name: "target_sheet", Type: "string", Desc: "Sheet to jump to inside this workbook (instead of url)."},
				property{Name: "target_address", Type: "string", Desc: "Cell to land on in that sheet (default A1)."},
				property{Name: "text", Type: "string", Desc: "Text to show in the cell. Omit to keep what is there."},
				property{Name: "screen_tip", Type: "string", Desc: "Hover text."},
			),
			Required: []string{"address"},
		},

		// ── 시트 ────────────────────────────────────────────────────────────────────
		{
			Name: "add_sheet",
			Desc: "Add a worksheet. Names must be unique, at most 31 characters, without : \\ / ? * [ ]. The new " +
				"sheet becomes the active one unless activate is false.",
			Props: []property{
				{Name: "name", Type: "string", Desc: "Tab name. Omit for Excel's default (Sheet2, …)."},
				{Name: "after", Type: "string", Desc: "Put it after this sheet. Omit for the end."},
				{Name: "activate", Type: "boolean", Desc: "Switch to it (default true)."},
			},
		},
		{
			Name: "delete_sheet",
			Desc: "Delete a worksheet and everything on it. Refused for the last sheet. There is no undo — " +
				"read_range or render_range first if you are not sure what is on it.",
			Props:    withSheet(),
			Required: []string{"sheet"},
		},
		{
			Name: "rename_sheet",
			Desc: "Rename a worksheet. Formulas that reference it keep working (Excel rewrites them).",
			Props: withSheet(
				property{Name: "name", Type: "string", Desc: "New tab name. Required."},
			),
			Required: []string{"sheet", "name"},
		},
		{
			Name: "move_sheet",
			Desc: "Move a worksheet to a 1-based tab position.",
			Props: withSheet(
				property{Name: "to", Type: "integer", Desc: "1-based position. Required."},
			),
			Required: []string{"sheet", "to"},
		},
		{
			Name: "copy_sheet",
			Desc: "Copy a worksheet inside this workbook, values and formats and all. The copy gets \" (2)\" appended " +
				"unless name is given. Needs ExcelApi 1.7.",
			Props: withSheet(
				property{Name: "name", Type: "string", Desc: "Name for the copy."},
				property{Name: "after", Type: "string", Desc: "Put the copy after this sheet (default: right after the original)."},
			),
			Required: []string{"sheet"},
		},
		{
			Name: "set_sheet_visibility",
			Desc: "Show or hide a worksheet. VeryHidden cannot be unhidden from the Excel UI — use it only when asked.",
			Props: withSheet(
				property{Name: "visibility", Type: "string", Desc: "Visible, Hidden, VeryHidden.", Enum: xlVisibilities},
			),
			Required: []string{"sheet", "visibility"},
		},
		{
			Name:     "activate_sheet",
			Desc:     "Bring a worksheet to the front — what the person sees. Optionally select a range on it.",
			Props:    withRange(),
			Required: []string{"sheet"},
		},
		{
			Name: "freeze_panes",
			Desc: "Freeze the top rows and/or left columns so headers stay while scrolling. rows:1 is the usual " +
				"call. Call it with neither rows nor columns to unfreeze (0 is refused). Needs ExcelApi 1.7.",
			Props: withSheet(
				property{Name: "rows", Type: "integer", Desc: "Rows to freeze from the top (default 0)."},
				property{Name: "columns", Type: "integer", Desc: "Columns to freeze from the left (default 0)."},
			),
		},
		{
			Name: "set_rows_columns",
			Desc: "Rows or columns as a whole: hide/show, group/ungroup (outline), height (rows, pt) or width (columns, pt). " +
				"Give `rows` like \"3:5\" or `columns` like \"B:D\". Only the fields you pass change. Grouping needs ExcelApi 1.10.",
			Props: withSheet(
				property{Name: "rows", Type: "string", Desc: "Row span, e.g. \"3:5\" (or \"7\")."},
				property{Name: "columns", Type: "string", Desc: "Column span, e.g. \"B:D\" (or \"B\")."},
				property{Name: "hidden", Type: "boolean", Desc: "true hides, false shows."},
				property{Name: "group", Type: "boolean", Desc: "true groups (outline level), false ungroups."},
				property{Name: "height", Type: "number", Desc: "Row height in points (rows only)."},
				property{Name: "width", Type: "number", Desc: "Column width in points (columns only)."},
			),
		},
		{
			Name:     "set_tab_color",
			Desc:     "Colour a worksheet's tab (#RRGGBB), or none to clear. Needs ExcelApi 1.7.",
			Props:    withSheet(property{Name: "color", Type: "string", Desc: "#RRGGBB or none. Required."}),
			Required: []string{"color"},
		},
		{
			Name: "set_sheet_view",
			Desc: "What a worksheet shows around the cells: gridlines on/off, row/column headings on/off. Needs ExcelApi 1.8.",
			Props: withSheet(
				property{Name: "gridlines", Type: "boolean"}, property{Name: "headings", Type: "boolean"},
			),
		},
		{
			Name: "set_workbook_properties",
			Desc: "Set the workbook's document properties: title, subject, author, keywords, comments, category. Only the " +
				"fields you pass change. Needs ExcelApi 1.7.",
			Props: []property{
				property{Name: "title", Type: "string"}, property{Name: "subject", Type: "string"}, property{Name: "author", Type: "string"},
				property{Name: "keywords", Type: "string"}, property{Name: "comments", Type: "string"}, property{Name: "category", Type: "string"},
			},
		},
		{
			Name: "set_page_setup",
			Desc: "How a worksheet prints: print area, orientation, fit to N pages wide/tall (0 = as many as needed), rows " +
				"repeated on every page (\"$1:$1\"), gridlines, centering, margins (pt). Only the fields you pass change. Needs ExcelApi 1.9.",
			Props: withSheet(
				property{Name: "print_area", Type: "string", Desc: "A1 range to print, or none to clear."},
				property{Name: "orientation", Type: "string", Desc: "Portrait or Landscape.", Enum: xlOrientations},
				property{Name: "fit_width", Type: "integer", Desc: "Pages wide (1 = shrink to one page wide; 0 = automatic)."},
				property{Name: "fit_height", Type: "integer", Desc: "Pages tall."},
				property{Name: "title_rows", Type: "string", Desc: "Rows repeated at the top of every page, e.g. \"$1:$1\"; none to clear."},
				property{Name: "gridlines", Type: "boolean", Desc: "Print gridlines."},
				property{Name: "center", Type: "boolean", Desc: "Center horizontally on the page."},
				property{Name: "margins", Type: "object", Desc: "{left, right, top, bottom} in points."},
			),
		},
		{
			Name: "protect_workbook",
			Desc: "Protect the workbook's STRUCTURE (no adding, deleting, renaming or moving sheets), optionally with a password; " +
				"protected:false lifts it. Cell edits are governed by protect_sheet. Needs ExcelApi 1.7.",
			Props: []property{
				property{Name: "protected", Type: "boolean", Desc: "Default true."},
				property{Name: "password", Type: "string", Desc: "Optional; needed again to unprotect."},
			},
		},
		{
			Name: "protect_sheet",
			Desc: "Protect a worksheet from edits (optionally with a password). Formatting/sorting stay allowed only " +
				"if you say so. Needs ExcelApi 1.7.",
			Props: withSheet(
				property{Name: "password", Type: "string", Desc: "Optional password. Remember: no password means anyone can unprotect from the UI."},
				property{Name: "allow_formatting", Type: "boolean"}, property{Name: "allow_sorting", Type: "boolean"}, property{Name: "allow_filtering", Type: "boolean"},
			),
		},
		{
			Name:  "unprotect_sheet",
			Desc:  "Remove worksheet protection (with the password if one was set).",
			Props: withSheet(property{Name: "password", Type: "string"}),
		},

		// ── 표 ──────────────────────────────────────────────────────────────────────
		{
			Name: "add_table",
			Desc: "Turn a range into a table (ListObject): filter buttons, banded rows, structured references. " +
				"The range must have a header row unless has_headers is false. If the range already belongs to a " +
				"table this is refused by name — write cells into the existing one instead. A style from the " +
				"workbook's theme looks better than anything you would draw.",
			Props: withRange(
				property{Name: "name", Type: "string", Desc: "Table name (letters, digits, underscore; no spaces). Omit for Table1, Table2 …"},
				property{Name: "has_headers", Type: "boolean", Desc: "First row is the header (default true)."},
				property{Name: "table_style", Type: "string", Desc: "Built-in style, e.g. TableStyleMedium2 (Excel's default look), TableStyleLight9.", Enum: xlTableStyles},
				property{Name: "show_totals", Type: "boolean", Desc: "Add a totals row."},
			),
			Required: []string{"address"},
		},
		{
			Name: "set_table_cells",
			Desc: "Write into a table by column name and 0-based body row — the table keeps its name, style and " +
				"filters. rows beyond the table's end are appended.",
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name. Required."},
				{Name: "cells", Type: "array", Items: "object", Desc: "[{row, column, value}] — row 0-based within the body, column a header name or 0-based index."},
			},
			Required: []string{"table", "cells"},
		},
		{
			Name: "add_table_rows",
			Desc: "Append rows to a table (or insert at a 0-based index). Each row is an array matching the " +
				"table's columns left to right.",
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name. Required."},
				{Name: "rows", Type: "array", Items: "array", Desc: "Rows of values. Required."},
				{Name: "at", Type: "integer", Desc: "0-based body row to insert before. Omit to append."},
			},
			Required: []string{"table", "rows"},
		},
		{
			Name: "edit_table",
			Desc: "Change a table's shape: add columns (by name, at the end), delete columns (by name), resize to a new range " +
				"(ExcelApi 1.13; must keep the header row), show/hide the totals row. Rows are added with add_table_rows; a table " +
				"becomes a plain range with remove_table.",
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name. Required."},
				{Name: "add_columns", Type: "array", Items: "string", Desc: "Header names of new columns, appended at the end."},
				{Name: "delete_columns", Type: "array", Items: "string", Desc: "Header names of columns to remove."},
				{Name: "resize", Type: "string", Desc: "New A1 range for the whole table including its header, e.g. \"A1:F30\"."},
				{Name: "show_totals", Type: "boolean", Desc: "Totals row on/off."},
			},
			Required: []string{"table"},
		},
		{
			Name: "remove_table",
			Desc: "Convert a table back to a plain range (data stays) — or delete its cells too when delete_data " +
				"is true.",
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name. Required."},
				{Name: "delete_data", Type: "boolean", Desc: "Also clear the cells (default false)."},
			},
			Required: []string{"table"},
		},
		{
			Name: "sort_range",
			Desc: "Sort a range (or a table by name) by one or more columns. Say has_headers so the header row " +
				"stays put. Sorting a range that overlaps a table is refused — sort the table.",
			Props: withRange(
				property{Name: "table", Type: "string", Desc: "Sort this table instead of a range."},
				property{Name: "by", Type: "array", Items: "object", Desc: "[{column, ascending}] — column is 0-based within the range (or a header name for a table); ascending default true. First entry wins ties."},
				property{Name: "has_headers", Type: "boolean", Desc: "First row is a header (default true)."},
			),
			Required: []string{"by"},
		},
		{
			Name: "filter_table",
			Desc: "Filter a table's column: keep rows whose value is in values, or matches a criterion. An empty " +
				"values with no criterion clears that column's filter. Needs ExcelApi 1.9.",
			Props: []property{
				{Name: "table", Type: "string", Desc: "Table name. Required."},
				{Name: "column", Type: "string", Desc: "Header name. Required."},
				{Name: "values", Type: "array", Items: "string", Desc: "Values to keep."},
				{Name: "criterion", Type: "string", Desc: "Instead of values: a comparison like \">1000\", \"<=0\", \"<>취소\", or \"top10\"/\"bottom10\"."},
			},
			Required: []string{"table", "column"},
		},

		// ── 차트 ────────────────────────────────────────────────────────────────────
		{
			Name: "add_chart",
			Desc: "Put a NATIVE Excel chart on a sheet from a source range that already holds the data (headers in " +
				"the first row/column become series and category names). It goes ON THE SHEET YOU NAME at " +
				"left/top; give width/height in points. ColumnClustered is the default; Korean names work " +
				"(막대·가로막대·꺾은선·원·분산·영역). An unknown type is refused with the list.",
			Props: withSheet(
				property{Name: "source", Type: "string", Desc: "A1 range holding the data, headers included (\"A1:C7\"). Required.", Also: []string{"address"}},
				property{Name: "chart_type", Type: "string", Desc: "ColumnClustered (default), ColumnStacked, BarClustered, BarStacked, Line, LineMarkers, Pie, Doughnut, Area, AreaStacked, XYScatter, Radar, Waterfall, Treemap, Sunburst, Funnel.", Enum: xlChartTypes, Also: []string{"kind", "type"}},
				property{Name: "series_by", Type: "string", Desc: "Columns (default) or Rows — which way the series run in source.", Enum: xlSeriesBys},
				property{Name: "title", Type: "string", Desc: "Chart title. Omit for none."},
				property{Name: "name", Type: "string", Desc: "Chart name to use later (\"매출 추이\"). Omit for Chart 1, 2 …"},
				property{Name: "left", Type: "number", Desc: "Points from the sheet's left edge (default: right of the source)."},
				property{Name: "top", Type: "number", Desc: "Points from the top (default: level with the source)."},
				property{Name: "width", Type: "number", Desc: "Points (default 480)."},
				property{Name: "height", Type: "number", Desc: "Points (default 300)."},
			),
			Required: []string{"source"},
		},
		{
			Name: "format_chart",
			Desc: "Change one chart's title, axis titles, legend, data labels, or move/resize it. Any subset.",
			Props: withSheet(
				property{Name: "chart", Type: "string", Desc: "Chart name. Required."},
				property{Name: "title", Type: "string", Desc: "Chart title; \"\" removes it."},
				property{Name: "x_title", Type: "string", Desc: "Category axis title."},
				property{Name: "y_title", Type: "string", Desc: "Value axis title."},
				property{Name: "legend", Type: "string", Desc: "Right, Left, Top, Bottom, or none.", Enum: xlLegendPositions},
				property{Name: "data_labels", Type: "boolean", Desc: "Show values on the points."},
				property{Name: "series", Type: "array", Items: "object", Desc: "[{index (0-based) or name, color (#RRGGBB), new_name, trendline: linear|none, marker: Circle|Square|Diamond|Triangle|None}] — per-series looks (color 1.1, trendline/marker 1.7)."},
				property{Name: "y_min", Type: "number", Desc: "Value axis minimum (1.7)."}, property{Name: "y_max", Type: "number", Desc: "Value axis maximum (1.7)."},
				property{Name: "y_format", Type: "string", Desc: "Value axis number format, e.g. \"#,##0\" (1.8)."},
				property{Name: "source", Type: "string", Desc: "Re-point the chart at another data range (A1 or Sheet!A1)."},
				property{Name: "chart_type", Type: "string", Desc: "Change the type." +
					" Korean/short names are accepted too: 막대, 가로막대, 꺾은선, 원, 도넛, 영역, 분산, 방사형, 누적, 폭포 (bar, hbar, line, pie, scatter…).", Enum: xlChartTypes},
				property{Name: "left", Type: "number"}, property{Name: "top", Type: "number"},
				property{Name: "width", Type: "number"}, property{Name: "height", Type: "number"},
			),
			Required: []string{"chart"},
		},
		{
			Name:     "delete_chart",
			Desc:     "Delete a chart by name.",
			Props:    withSheet(property{Name: "chart", Type: "string", Desc: "Chart name. Required."}),
			Required: []string{"chart"},
		},

		// ── 조건부 서식·유효성·이름·메모 ───────────────────────────────────────────────
		{
			Name: "add_conditional_format",
			Desc: "Add a conditional format to a range: cell_value (compare to a number/text, then colour), " +
				"color_scale (2- or 3-colour gradient), data_bar, icon_set, contains_text, top_bottom (top/bottom N " +
				"or %), or custom (an Excel formula that is TRUE where the format applies). Colours as #RRGGBB. " +
				"Needs ExcelApi 1.6 (color_scale/data_bar/icon_set/top_bottom 1.6; custom 1.6).",
			Props: withRange(
				property{Name: "cf_kind", Type: "string", Desc: "cell_value, color_scale, data_bar, icon_set, contains_text, top_bottom, custom.", Enum: xlCfKinds, Also: []string{"kind"}},
				property{Name: "operator", Type: "string", Desc: "For cell_value/contains_text: Between, NotBetween, EqualTo, NotEqualTo, GreaterThan, LessThan, GreaterThanOrEqualTo, LessThanOrEqualTo (cell_value); Contains, NotContains, BeginsWith, EndsWith (contains_text).", Enum: xlCfOperators},
				property{Name: "value", Type: "string", Desc: "Comparison value (\"1000\", \"=B$1\", \"취소\"). For Between, the low end."},
				property{Name: "value2", Type: "string", Desc: "High end for Between/NotBetween."},
				property{Name: "formula", Type: "string", Desc: "For custom: an Excel formula relative to the top-left cell, e.g. \"=$C2>$D2\"."},
				property{Name: "fill", Type: "string", Desc: "Fill colour #RRGGBB when the rule matches."},
				property{Name: "color", Type: "string", Desc: "Font colour #RRGGBB when the rule matches."},
				property{Name: "bold", Type: "boolean"},
				property{Name: "colors", Type: "array", Items: "string", Desc: "For color_scale: 2 or 3 colours low→high (default green-yellow-red style). For data_bar: one colour."},
				property{Name: "icon_set", Type: "string", Desc: "For icon_set: ThreeArrows, ThreeTrafficLights1, ThreeSymbols, FourArrows, FiveArrows, FiveRating …"},
				property{Name: "rank", Type: "integer", Desc: "For top_bottom: N (default 10)."},
				property{Name: "bottom", Type: "boolean", Desc: "For top_bottom: bottom N instead of top."},
				property{Name: "percent", Type: "boolean", Desc: "For top_bottom: N is a percentage."},
			),
			Required: []string{"address", "cf_kind"},
		},
		{
			Name:  "clear_conditional_formats",
			Desc:  "Remove every conditional format that touches a range (omit address for the whole sheet).",
			Props: withRange(),
		},
		{
			Name: "set_validation",
			Desc: "Data validation on a range: list (a dropdown from values or a range), whole_number, decimal, date, " +
				"time, text_length (each with an operator and bounds), or custom (a formula). Optional input prompt " +
				"and error message. clear:true removes validation. Needs ExcelApi 1.8.",
			Props: withRange(
				property{Name: "validation_kind", Type: "string", Desc: "list, whole_number, decimal, date, time, text_length, custom.", Enum: xlValidationKinds, Also: []string{"kind"}},
				property{Name: "values", Type: "array", Items: "string", Desc: "For list: the choices."},
				property{Name: "source", Type: "string", Desc: "For list: a range holding the choices instead of values (\"=Lists!$A$1:$A$5\")."},
				property{Name: "operator", Type: "string", Desc: "For number/date/time/length kinds: Between (default), NotBetween, EqualTo, NotEqualTo, GreaterThan, LessThan, GreaterThanOrEqualTo, LessThanOrEqualTo.", Enum: xlValidationOperators},
				property{Name: "value", Type: "string", Desc: "Bound (or the low end for Between)."},
				property{Name: "value2", Type: "string", Desc: "High end for Between/NotBetween."},
				property{Name: "formula", Type: "string", Desc: "For custom."},
				property{Name: "prompt", Type: "string", Desc: "Input message shown when the cell is selected."},
				property{Name: "error", Type: "string", Desc: "Message when the entry is rejected."},
				property{Name: "clear", Type: "boolean", Desc: "Remove validation from the range instead."},
			),
			Required: []string{"address"},
		},
		{
			Name: "set_name",
			Desc: "Define a named item (\"매출합계\" → \"=Sheet1!$E$20\", or a constant \"=0.1\"). Workbook scope unless " +
				"sheet is given. An existing name is updated.",
			Props: withSheet(
				property{Name: "name", Type: "string", Desc: "The name. Required."},
				property{Name: "refers_to", Type: "string", Desc: "A range (\"=Sheet1!$A$1:$B$9\") or a value/formula. Required."},
				property{Name: "comment", Type: "string", Desc: "What it is for."},
			),
			Required: []string{"name", "refers_to"},
		},
		{
			Name:     "delete_name",
			Desc:     "Delete a named item. Formulas that used it will show #NAME? — read_names first.",
			Props:    withSheet(property{Name: "name", Type: "string", Desc: "The name. Required."}),
			Required: []string{"name"},
		},
		{
			Name: "add_comment",
			Desc: "Add a threaded comment on a cell, or reply to the one already there. Needs ExcelApi 1.10. On Excel 2021 for Windows " +
				"(no threaded-comment API) the helper writes a cell NOTE instead — a reply is appended to the note — and the answer says so (kind: note).",
			Props: withRange(
				property{Name: "text", Type: "string", Desc: "Comment text. Required."},
			),
			Required: []string{"address", "text"},
		},
		{
			Name: "resolve_comment",
			Desc: "Mark the comment thread on a cell resolved (or reopen it with resolved:false), or delete it. " +
				"Needs ExcelApi 1.11 (resolve) / 1.10 (delete). On Excel 2021 for Windows the cell holds a NOTE instead: notes cannot be " +
				"resolved, only deleted (delete:true).",
			Props: withRange(
				property{Name: "resolved", Type: "boolean", Desc: "Default true."},
				property{Name: "delete", Type: "boolean", Desc: "Delete the thread instead."},
			),
			Required: []string{"address"},
		},

		// ── 그림·피벗 ────────────────────────────────────────────────────────────────
		{
			Name: "add_image",
			Desc: "Put a picture from the person's own computer on a sheet. Give the FILE PATH — never base64: the " +
				"helper reads the file itself. The file is checked by its CONTENT and anything that is not a real " +
				"PNG/JPEG/GIF/BMP is refused. Needs ExcelApi 1.9.",
			Props: withSheet(
				property{Name: "path", Type: "string", Desc: "Where the picture is on this machine. Required."},
				property{Name: "address", Type: "string", Desc: "Cell whose top-left corner anchors the picture, e.g. \"E10\". Overrides left/top."},
				property{Name: "left", Type: "number", Desc: "Points (default 20)."}, property{Name: "top", Type: "number", Desc: "Points (default 20)."},
				property{Name: "width", Type: "number", Desc: "Points. Omit both width and height to keep the natural size (capped to fit)."},
				property{Name: "height", Type: "number"},
				property{Name: "name", Type: "string", Desc: "Shape name (default 그림)."},
				property{Name: "alt", Type: "string", Desc: "Alt text. Set it — screen readers read this, not the file name."},
			),
			Required: []string{"path"},
		},
		{
			Name: "add_pivot",
			Desc: "Create a PivotTable from a source range (headers in the first row) at a destination cell on a " +
				"sheet: rows and columns are field names, values are {field, function}. Needs ExcelApi 1.8. Refused " +
				"if the destination overlaps existing data.",
			Props: withSheet(
				property{Name: "source", Type: "string", Desc: "Data range with headers, e.g. \"Data!A1:F200\" (sheet-qualified is allowed here). Required."},
				property{Name: "destination", Type: "string", Desc: "Top-left cell for the pivot on sheet (\"H2\"). Required."},
				property{Name: "name", Type: "string", Desc: "Pivot name."},
				property{Name: "rows", Type: "array", Items: "string", Desc: "Row fields (header names)."},
				property{Name: "columns", Type: "array", Items: "string", Desc: "Column fields."},
				property{Name: "values", Type: "array", Items: "object", Desc: "[{field, function, number_format, name}] — function: Sum (default), Count, Average, Max, Min; number_format like \"#,##0\"; name is the shown label.", Also: []string{"data"}},
			),
			Required: []string{"source", "destination"},
		},
		{
			Name: "trace_cell",
			Desc: "Where a cell's value comes from (precedents: the cells its formula reads) or goes to (dependents: cells " +
				"whose formulas read it) — the way into \"why is this number what it is\". One cell. Needs ExcelApi 1.12 " +
				"(precedents) / 1.13 (dependents)." + declare,
			Props:    withRange(property{Name: "what", Type: "string", Desc: "precedents (default) or dependents.", Enum: xlTraceWhats}),
			Required: []string{"address"},
			ReadOnly: true,
		},
		{
			Name: "insert_sheets_from_file",
			Desc: "Copy every worksheet of another workbook (.xlsx on this machine) into this one, at the end or after a " +
				"sheet. Say the path; the helper reads the file and refuses anything that is not an Excel workbook. Needs ExcelApi 1.13.",
			Props: []property{
				property{Name: "path", Type: "string", Desc: "Where the .xlsx is on this machine. Required."},
				property{Name: "after", Type: "string", Desc: "Insert after this sheet (default: at the end)."},
			},
			Required: []string{"path"},
		},
		{
			Name: "import_csv",
			Desc: "Write a CSV file's rows into a sheet starting at a cell (default A1 of a new sheet named after the file). " +
				"Numbers stay numbers, everything else is text. Say the path; the helper reads and parses it.",
			Props: withSheet(
				property{Name: "path", Type: "string", Desc: "Where the .csv is on this machine. Required."},
				property{Name: "address", Type: "string", Desc: "Top-left cell (default A1)."},
			),
			Required: []string{"path"},
		},
		{
			Name:  "refresh_pivot",
			Desc:  "Refresh one pivot (by name) or every pivot on the sheet when name is omitted.",
			Props: withSheet(property{Name: "name", Type: "string", Desc: "Pivot name."}),
		},

		// ── 되돌리기·메모·제안 ───────────────────────────────────────────────────────
		{
			Name: "restore_range",
			Desc: "Put a snapshot back — values, formulas and number formats. Refuses a snapshot this session " +
				"does not hold.",
			Props: []property{
				{Name: "snapshot", Type: "string", Desc: "Snapshot id from snapshot_range. Required."},
			},
			Required: []string{"snapshot"},
		},
		{
			Name: "set_tag",
			Desc: "Leave a note in the workbook that stays in the file and never shows (workbook settings). Use it " +
				"for what you will want to know later: that you built this sheet, what the person asked. Omit " +
				"value to delete.",
			Props: []property{
				{Name: "key", Type: "string", Desc: "Name of the note. Required."},
				{Name: "value", Type: "string", Desc: "What to remember. Omit to delete."},
			},
			Required: []string{"key"},
		},
		{
			Name: "suggest",
			Desc: "Attach a fix-suggestion to a sheet or range, the way a person leaves a comment in Word. It is " +
				"written INTO THE WORKBOOK (settings) and shows as a card in the task pane with Apply. THIS DOES " +
				"NOT CHANGE THE WORKBOOK. fix may name one of: write_range, format_range, set_number_format, " +
				"autofit, add_conditional_format, sort_range.",
			Props: withRange(
				property{Name: "what", Type: "string", Desc: "The suggestion, one sentence. Required."},
				property{Name: "why", Type: "string", Desc: "Why it would be better."},
				property{Name: "fix", Type: "object", Desc: "{tool, args} — the call Apply should make."},
			),
			Required: []string{"what"},
		},
		{
			Name:     "drop_suggestion",
			Desc:     "Take one suggestion off without doing it. Refuses a key that is not a suggestion.",
			Props:    []property{{Name: "key", Type: "string", Desc: "The suggestion's key, from read_suggestions. Required."}},
			Required: []string{"key"},
		},
	}
}

// xlDocumentProp 는 모든 도구가 같이 받는 칸이다(MCP 에 scope 개념이 없으니 인자로 받는다).
var xlDocumentProp = property{
	Name: "document",
	Type: "string",
	Desc: "Omit it. This conversation is bound to one workbook and every call goes to that workbook. Only a hub-wide conversation (no workbook of its own) names one here, with a key from an earlier answer.",
	Also: []string{"workbook"},
}
