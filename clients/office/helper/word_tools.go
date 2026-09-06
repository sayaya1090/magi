package office

var fromProp = property{Name: "from", Type: "integer", Desc: "First paragraph, 1-based (from list_paragraphs). Omit for the whole body."}
var toProp = property{Name: "to", Type: "integer", Desc: "Last paragraph, inclusive. Omit for just `from`."}
var paraProp = property{Name: "paragraph", Type: "integer", Desc: "A paragraph number, 1-based (from list_paragraphs).", Also: []string{"para"}}
var tableProp = property{Name: "table", Type: "integer", Desc: "Table number, 1-based in body order (from list_paragraphs / read_document)."}

func withFromTo(rest ...property) []property {
	return append([]property{fromProp, toProp}, rest...)
}

func wordCatalogue(hasCouncil bool) []tool {
	declare := ""
	if hasCouncil {
		declare = " A turn that called any tool must end by declaring it finished with " +
			"council{complete:true}, even a read-only one: otherwise the turn lands UNVERIFIED."
	}
	return []tool{
		{
			Name: "list_paragraphs",
			Desc: "A DOCUMENT IS ALREADY OPEN IN WORD AND THESE TOOLS ARE ATTACHED TO IT. You do not " +
				"create, open or upload a document, and there is no tool that does — the person is looking at " +
				"theirs right now and every tool here edits that one. Never ask them to provide, upload or " +
				"attach a file. If a call fails, read the error and RETRY IT. Do not fall back to building a " +
				"document any other way: a .docx you write with a shell, python-docx or COM automation is a " +
				"FILE NOBODY IS LOOKING AT, and the person ends up with an unchanged document on screen plus " +
				"scripts they did not ask for. If these tools truly cannot do it, say so and stop. " +
				"THE DOCUMENT'S OUTLINE: every body paragraph with its 1-based number, style (Heading 1, Normal, " +
				"List Paragraph…), list level, whether it sits in a table (and which), and the first ~80 " +
				"characters. Read this first; it is one call and it tells you where everything is. Long " +
				"documents are paged (from/to/max)." + declare,
			Props:    withFromTo(property{Name: "max", Type: "integer", Desc: "Rows to return (default 200)."}),
			ReadOnly: true,
		},
		{
			Name: "read_paragraphs",
			Desc: "Full text of paragraphs from..to, one entry per paragraph, with style, alignment, list level and " +
				"the font of the first run (name, size, bold, italic, color). Use after list_paragraphs to read " +
				"the passage you are about to change. Cheap for a page, expensive for a book — page it." + declare,
			Props:    withFromTo(property{Name: "max_chars", Type: "integer", Desc: "Cap per paragraph (default 4000)."}),
			ReadOnly: true,
		},
		{
			Name: "read_document",
			Desc: "The document as a whole: title/subject/author/keywords, counts (paragraphs, tables, sections, " +
				"inline pictures, comments where the host can count them), each section's header and footer text, " +
				"the change-tracking mode, and the requirement sets this host supports (WordApi 1.3 is Word " +
				"2019/2021; comments, bookmarks and tracked changes need 1.4+, i.e. Microsoft 365 / 2024)." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "find",
			Desc: "Search the body for text. Answers paragraph numbers and the matched text with a little context, " +
				"so you can address the passage with from/to or replace it with replace_all. Case-insensitive " +
				"unless match_case; whole-word optional. Wildcards are OFF." + declare,
			Props: []property{
				property{Name: "text", Type: "string", Desc: "What to look for. Required."},
				property{Name: "match_case", Type: "boolean"}, property{Name: "whole_word", Type: "boolean"},
				property{Name: "limit", Type: "integer", Desc: "Max hits (default 50)."},
			},
			Required: []string{"text"},
			ReadOnly: true,
		},
		{
			Name: "read_table",
			Desc: "One table: its number, size, header row flag, style, and every cell's text as a 2-D array " +
				"(rows of cells). Cells that were merged read as empty in the covered positions." + declare,
			Props:    []property{tableProp, property{Name: "max_rows", Type: "integer", Desc: "Rows to return (default 200)."}},
			Required: []string{"table"},
			ReadOnly: true,
		},
		{
			Name: "read_html",
			Desc: "The passage from..to as HTML, as Word renders it. This is the LOOK — the nearest thing to seeing " +
				"the page: it carries bold, sizes, colors, lists and tables where read_paragraphs gives you " +
				"numbers. Word cannot render a page to an image, so this is what you check formatting with. " +
				"Big — ask for a few paragraphs at a time." + declare,
			Props:    withFromTo(property{Name: "max_chars", Type: "integer", Desc: "Cap on the HTML (default 20000)."}),
			ReadOnly: true,
		},
		{
			Name: "read_comments",
			Desc: "Every comment thread on the body: id, author, date, the commented text, the comment, its " +
				"replies, and whether it is resolved. Needs WordApi 1.4 (Microsoft 365 / 2024) — on 2019/2021 " +
				"this refuses by name." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "list_images",
			Desc: "Every inline picture in the body: number (document order), the paragraph it sits in, width and height " +
				"(pt), alt text. Use the number with format_image / delete_image." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "read_footnotes",
			Desc: "Every footnote and endnote: number, kind, the paragraph it hangs on, the referenced text, and the note. " +
				"Needs WordApi 1.5 — refused by name on 2019/2021." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "render_page",
			Desc: "See one page of the document as a picture — Word hands the whole document over as PDF and the helper " +
				"draws the asked page (needs poppler's pdftoppm on this machine; a Mac without it draws page 1 only). Use " +
				"it to check layout after a batch of edits; read_html is the cheaper look at formatting." + declare,
			Props: []property{
				property{Name: "page", Type: "integer", Desc: "Page number, 1-based (default 1)."},
				property{Name: "max_width", Type: "integer", Desc: "Picture width in pixels (default 800)."},
			},
			ReadOnly: true,
		},
		{
			Name: "read_content_controls",
			Desc: "Every content control in the body — id, tag, title, type, the text inside, placeholder, whether editing/" +
				"deleting is locked, and the paragraph it starts in. Templates and forms live here; set_content_control fills them." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "read_tracked_changes",
			Desc: "Pending tracked changes (insertions, deletions, formatting) with author, date and text, plus the " +
				"tracking mode. Needs WordApi 1.6 (Microsoft 365 / 2024)." + declare,
			Props:    withFromTo(property{Name: "limit", Type: "integer", Desc: "Max changes (default 100)."}),
			ReadOnly: true,
		},
		{
			Name: "describe_style",
			Desc: "How this document is dressed: the paragraph styles in use with counts, the body font (name, size), " +
				"heading fonts, and the first few heading texts. Read it before adding to a document you did not " +
				"write, so new text matches — use the SAME style names, not ad-hoc bold." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "snapshot_paragraphs",
			Desc: "Take a snapshot of paragraphs from..to (their OOXML) and get an id, so restore_paragraphs can put " +
				"them back exactly. Take one before a replace_all or delete you are not sure of. Snapshots live in " +
				"the task pane's memory only — they do not survive the pane closing." + declare,
			Props:    withFromTo(),
			ReadOnly: true,
		},
		{
			Name: "read_tags",
			Desc: "Notes earlier turns left in the document (custom document properties under MAGI.*): what was " +
				"decided, what was left. Read them before continuing someone else's work." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "read_suggestions",
			Desc: "Suggestions pinned to this document by suggest (see it) — what they propose, where, and whether " +
				"the pane can apply them. Needs WordApi 1.4 (settings)." + declare,
			Props:    []property{},
			ReadOnly: true,
		},
		{
			Name: "advise",
			Desc: "Pin advice to the task pane for the person — things you noticed but did NOT change (an " +
				"inconsistent heading, a table without a header row). Each item names a paragraph so the " +
				"pane can jump to it. Changes nothing in the document." + declare,
			Props: []property{property{Name: "items", Type: "array", Items: "object",
				Desc: "[{message, why?, paragraph?}] — paragraph is 1-based; omit it if the advice is about the whole document."}},
			Required: []string{"items"},
			ReadOnly: true,
		},
		{
			Name:     "clear_advice",
			Desc:     "Remove the pinned advice from the task pane." + declare,
			Props:    []property{},
			ReadOnly: true,
		},

		// ── 쓰기 ──
		{
			Name: "insert_paragraphs",
			Desc: "Insert paragraphs. `lines` is an array of strings, one paragraph each (an empty string is a " +
				"blank paragraph); `style` applies to all of them (\"Heading 1\", \"Normal\", \"List Paragraph\" — " +
				"the document's own names, see describe_style). Where: after/before a paragraph number, or at the " +
				"start/end of the body. Answers the new paragraphs' numbers. This is also how you write a whole " +
				"section: headings and body in one call, in order.",
			Props: []property{
				property{Name: "lines", Type: "array", Items: "string", Desc: "Paragraph texts in order. Required."},
				property{Name: "after", Type: "integer", Desc: "Insert after this paragraph number (default: end of body)."},
				property{Name: "before", Type: "integer", Desc: "Insert before this paragraph number."},
				property{Name: "at", Type: "string", Desc: "\"start\" or \"end\" of the body when neither after nor before is given.", Enum: wordAtWhere},
				property{Name: "style", Type: "string", Desc: "Style for all lines: a built-in name (\"Heading2\", \"Normal\", \"ListParagraph\" — language-independent) or the document's own style name."},
			},
			Required: []string{"lines"},
		},
		{
			Name: "replace_paragraph",
			Desc: "Replace the text of one paragraph, keeping its style and paragraph formatting. Character " +
				"formatting inside it (a bold word) is lost — use format_text afterwards if you need it back.",
			Props:    []property{paraProp, property{Name: "text", Type: "string", Desc: "The new text. Required.", Also: []string{"content"}}},
			Required: []string{"paragraph", "text"},
		},
		{
			Name: "delete_paragraphs",
			Desc: "Delete paragraphs from..to. There is no undo except a snapshot you took first — snapshot_paragraphs " +
				"if you are not sure. Refuses to delete the only paragraph in the body.",
			Props:    withFromTo(),
			Required: []string{"from"},
		},
		{
			Name: "set_style",
			Desc: "Apply a paragraph style to paragraphs from..to: the document's own style names (\"Heading 1\", " +
				"\"Title\", \"Quote\", \"List Paragraph\", \"Normal\") or a built-in name from `builtin`. Styles " +
				"are how headings become headings — the navigation pane, the table of contents and the outline " +
				"all read them; bold 16pt text is not a heading.",
			Props: withFromTo(
				property{Name: "style", Type: "string", Desc: "Style name as the document shows it (localized names work); a built-in name like \"Heading2\" is taken as builtin."},
				property{Name: "builtin", Type: "string", Desc: "Built-in style, language-independent.", Enum: wordBuiltinStyles},
			),
			Required: []string{"from"},
		},
		{
			Name: "format_text",
			Desc: "Character formatting for a passage: bold, italic, underline, strike, size (pt), color (#RRGGBB), " +
				"highlight (color name or \"none\"), font name. Give paragraphs from..to, or `text` to format only " +
				"the matches of that text inside them. Only the fields you pass change. Prefer set_style for " +
				"whole-paragraph looks.",
			Props: withFromTo(
				property{Name: "text", Type: "string", Desc: "Format only this text where it occurs in from..to (case-sensitive)."},
				property{Name: "bold", Type: "boolean"}, property{Name: "italic", Type: "boolean"}, property{Name: "strike", Type: "boolean"},
				property{Name: "underline", Type: "string", Desc: "None, Single, Double, Dotted, Dashed, Wave, Thick.", Enum: wordUnderlines},
				property{Name: "size", Type: "number", Desc: "Points."},
				property{Name: "color", Type: "string", Desc: "#RRGGBB.", Also: []string{"font_color"}},
				property{Name: "highlight", Type: "string", Desc: "Yellow, BrightGreen, Turquoise, Pink, Blue, Red, DarkBlue, Teal, Green, Violet, DarkRed, DarkYellow, Gray50, Gray25, Black, or none.", Enum: wordHighlights},
				property{Name: "font", Type: "string", Desc: "Font name, e.g. \"맑은 고딕\"."},
				property{Name: "clear", Type: "boolean", Desc: "Remove direct character formatting first."},
			),
			Required: []string{"from"},
		},
		{
			Name: "format_paragraph",
			Desc: "Paragraph formatting for from..to: alignment, space before/after (pt), line spacing (pt, e.g. 18), " +
				"first-line and left indents (pt). Only the fields you pass change.",
			Props: withFromTo(
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right, Justified.", Enum: wordAligns},
				property{Name: "space_before", Type: "number"}, property{Name: "space_after", Type: "number"},
				property{Name: "line_spacing", Type: "number", Desc: "Points between lines (12 = single for 12pt text; 18 = 1.5)."},
				property{Name: "first_line_indent", Type: "number"}, property{Name: "left_indent", Type: "number"}, property{Name: "right_indent", Type: "number"},
			),
			Required: []string{"from"},
		},
		{
			Name: "insert_table",
			Desc: "Insert a table after a paragraph (or at the end): `values` is a 2-D array (rows of cells, strings); " +
				"the first row is the header when has_header (default true). Optional built-in style. Answers the " +
				"table number. Put a caption paragraph before it yourself if the document uses them.",
			Props: []property{
				property{Name: "values", Type: "array", Items: "array", Desc: "Rows of cell texts, e.g. [[\"분기\",\"매출\"],[\"1분기\",\"12,000\"]]. Required."},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "has_header", Type: "boolean", Desc: "First row is a header (default true)."},
				property{Name: "table_style", Type: "string", Desc: "Built-in table style, e.g. GridTable4_Accent1, PlainTable1, TableGrid.", Enum: wordTableStyles},
			},
			Required: []string{"values"},
		},
		{
			Name: "set_table_cells",
			Desc: "Write individual cells of a table by 0-based row and column: [{row, column, value}]. Rows and " +
				"columns are counted including the header row.",
			Props:    []property{tableProp, property{Name: "cells", Type: "array", Items: "object", Desc: "[{row, column, value}] — 0-based. Required."}},
			Required: []string{"table", "cells"},
		},
		{
			Name: "add_table_rows",
			Desc: "Append rows to a table (at the end unless at=\"start\"): `rows` is a 2-D array with one entry per " +
				"column. Shorter rows are padded with empty cells.",
			Props: []property{tableProp,
				property{Name: "rows", Type: "array", Items: "array", Desc: "Rows of cell texts. Required."},
				property{Name: "at", Type: "string", Desc: "\"end\" (default) or \"start\".", Enum: wordAtWhere}},
			Required: []string{"table", "rows"},
		},
		{
			Name:     "delete_table",
			Desc:     "Delete a whole table. No undo — snapshot the paragraphs around it first if unsure.",
			Props:    []property{tableProp},
			Required: []string{"table"},
		},
		{
			Name: "format_table",
			Desc: "Table looks: built-in style, header row on/off, banded rows/columns, alignment of the table on the " +
				"page, and optional column widths (pt). Only the fields you pass change.",
			Props: []property{tableProp,
				property{Name: "table_style", Type: "string", Enum: wordTableStyles},
				property{Name: "header_row", Type: "boolean"}, property{Name: "banded_rows", Type: "boolean"}, property{Name: "banded_columns", Type: "boolean"},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right.", Enum: wordAligns},
				property{Name: "widths", Type: "array", Items: "number", Desc: "Column widths in points, left to right."}},
			Required: []string{"table"},
		},
		{
			Name: "format_table_cells",
			Desc: "Look of table CELLS: fill colour, text colour, bold, italic, size, horizontal and vertical alignment, " +
				"column width. Target explicit `cells` or a rectangle by `rows`/`columns` ranges (0-based, inclusive; omit " +
				"both for the whole table). Only the fields you pass change. Table looks as a whole: format_table.",
			Props: []property{tableProp,
				property{Name: "cells", Type: "array", Items: "object", Desc: "[{row, column}] 0-based, like set_table_cells."},
				property{Name: "rows", Type: "array", Items: "integer", Desc: "[first, last] row range, 0-based inclusive; one number = that row."},
				property{Name: "columns", Type: "array", Items: "integer", Desc: "[first, last] column range, 0-based inclusive."},
				property{Name: "fill", Type: "string", Desc: "Cell shading #RRGGBB, or none."},
				property{Name: "color", Type: "string", Desc: "Text colour #RRGGBB."},
				property{Name: "bold", Type: "boolean"}, property{Name: "italic", Type: "boolean"},
				property{Name: "size", Type: "number", Desc: "Points."},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right, Justified.", Enum: wordAligns},
				property{Name: "valign", Type: "string", Desc: "Top, Center, Bottom.", Enum: wordVAligns},
				property{Name: "width", Type: "number", Desc: "Column width in points for the targeted columns."},
			},
			Required: []string{"table"},
		},
		{
			Name: "edit_table",
			Desc: "Change a table's shape: delete rows or columns (0-based indexes), add columns (at end, start, or after " +
				"an index, with optional values top to bottom), merge a rectangle of cells (WordApi 1.4). Rows are added " +
				"with add_table_rows.",
			Props: []property{tableProp,
				property{Name: "delete_rows", Type: "array", Items: "integer", Desc: "Row indexes to remove, 0-based."},
				property{Name: "delete_columns", Type: "array", Items: "integer", Desc: "Column indexes to remove, 0-based."},
				property{Name: "add_columns", Type: "object", Desc: "{at: \"end\" | \"start\" | <column index to insert after>, count, values: [[…per new column, top to bottom]]}."},
				property{Name: "merge", Type: "object", Desc: "{from_row, from_column, to_row, to_column} 0-based inclusive."},
			},
			Required: []string{"table"},
		},
		{
			Name: "insert_list",
			Desc: "Insert a bulleted or numbered list after a paragraph (or at the end): one item per entry of " +
				"`items`; `levels` (same length, 0-based) nests. Answers the new paragraphs' numbers. To turn " +
				"existing paragraphs into a list use set_list.",
			Props: []property{
				property{Name: "items", Type: "array", Items: "string", Desc: "Item texts in order. Required."},
				property{Name: "kind", Type: "string", Desc: "bulleted (default) or numbered.", Enum: wordListKinds},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "levels", Type: "array", Items: "integer", Desc: "Nesting level per item, 0-based."},
			},
			Required: []string{"items"},
		},
		{
			Name: "set_list",
			Desc: "Make paragraphs from..to a list (kind bulleted/numbered), change their level, or take them out of " +
				"their list (detach). Paragraphs already in a list keep it unless kind is given.",
			Props: withFromTo(
				property{Name: "kind", Type: "string", Enum: wordListKinds},
				property{Name: "level", Type: "integer", Desc: "0-based list level."},
				property{Name: "detach", Type: "boolean", Desc: "Remove from the list."}),
			Required: []string{"from"},
		},
		{
			Name: "insert_image",
			Desc: "Put a picture from the person's own computer into the document, inline, after a paragraph. Give " +
				"the FILE PATH — never base64: the helper reads the file itself and refuses anything that is " +
				"not a real PNG/JPEG/GIF/BMP.",
			Props: []property{
				property{Name: "path", Type: "string", Desc: "Where the picture is on this machine. Required."},
				property{Name: "after", Type: "integer", Desc: "Paragraph number to insert after (default: end of body)."},
				property{Name: "width", Type: "number", Desc: "Points. Omit to keep the natural size (capped to the page width)."},
				property{Name: "alt", Type: "string", Desc: "Alt text. Set it."},
			},
			Required: []string{"path"},
		},
		{
			Name: "format_image",
			Desc: "Resize or relabel one inline picture (number from list_images): width and/or height in points (aspect " +
				"kept when only one is given), alt text, and the paragraph's alignment (Left, Centered, Right).",
			Props: []property{
				property{Name: "image", Type: "integer", Desc: "Picture number, 1-based in document order. Required."},
				property{Name: "width", Type: "number", Desc: "Points."}, property{Name: "height", Type: "number", Desc: "Points."},
				property{Name: "alt", Type: "string", Desc: "Alt text."},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right — of the paragraph holding it.", Enum: wordAligns},
			},
			Required: []string{"image"},
		},
		{
			Name:     "delete_image",
			Desc:     "Remove one inline picture by number (list_images). The paragraph stays; its text (if any) stays.",
			Props:    []property{property{Name: "image", Type: "integer", Desc: "Picture number, 1-based. Required."}},
			Required: []string{"image"},
		},
		{
			Name:     "insert_break",
			Desc:     "Insert a page break, a section break (next page) or a line break after a paragraph.",
			Props:    []property{paraProp, property{Name: "kind", Type: "string", Desc: "page (default), section, line.", Enum: wordBreakKinds}},
			Required: []string{"paragraph"},
		},
		{
			Name: "insert_field",
			Desc: "Insert a Word field — a table of contents, a page number, the page count, today's date or time, or a " +
				"document property — that Word keeps up to date. Goes into the body after/before a paragraph, or into a " +
				"section's header or footer (`which`). `template` writes text around fields: \"{page} / {pages}\" in a footer, " +
				"\"작성일 {date}\" in the body. A table of contents needs heading styles (set_style builtin Heading1…) to " +
				"list anything. Needs WordApi 1.5 (Word 2021 is 1.3 — refused there, nothing changed).",
			Props: []property{
				property{Name: "field", Type: "string", Desc: "toc, page, num_pages, date, time, title, author, file_name. Omit when template names them.", Enum: wordFieldKinds},
				property{Name: "template", Type: "string", Desc: "Text with {page} {pages} {date} {time} {title} {author} {file} placeholders, e.g. \"{page} / {pages}\"."},
				property{Name: "after", Type: "integer", Desc: "Body: put it in a new paragraph after this paragraph (1-based)."},
				property{Name: "before", Type: "integer", Desc: "Body: put it in a new paragraph before this paragraph."},
				property{Name: "at", Type: "string", Desc: "Body: start or end (default end) when neither after nor before is given.", Enum: wordAtWhere},
				property{Name: "which", Type: "string", Desc: "header or footer — then it goes there (appended as a new line) instead of the body.", Enum: wordHeaderFooter},
				property{Name: "section", Type: "integer", Desc: "With which: section number, 1-based (default 1)."},
				property{Name: "kind", Type: "string", Desc: "With which: Primary (default), FirstPage, EvenPages.", Enum: wordHeaderKinds},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right, Justified.", Enum: wordAligns},
				property{Name: "levels", Type: "string", Desc: "toc only: heading levels to list, e.g. \"1-3\" (default)."},
			},
		},
		{
			Name: "move_paragraphs",
			Desc: "Move a block of paragraphs (from…to, tables included) to another place: after/before a paragraph outside " +
				"the block, or `at` start/end. Formatting travels with it. Answers the block's new numbers — every number " +
				"between the old and new place shifts, so re-read list_paragraphs before touching neighbours.",
			Props: []property{
				property{Name: "from", Type: "integer", Desc: "First paragraph of the block, 1-based. Required (Omit `to` to move just this one).", Also: []string{"paragraph"}},
				property{Name: "to", Type: "integer", Desc: "Last paragraph of the block, inclusive (default: from)."},
				property{Name: "after", Type: "integer", Desc: "Put the block after this paragraph (numbers as they are NOW)."},
				property{Name: "before", Type: "integer", Desc: "Put the block before this paragraph."},
				property{Name: "at", Type: "string", Desc: "start or end of the body when neither after nor before is given.", Enum: wordAtWhere},
			},
			Required: []string{"from"},
		},
		{
			Name: "insert_file",
			Desc: "Insert another Word document (.docx on this machine) into this one — after/before a paragraph or at " +
				"start/end — with its text, tables and formatting. Say the path; the helper reads the file and refuses " +
				"anything that is not a Word document.",
			Props: []property{
				property{Name: "path", Type: "string", Desc: "Where the .docx is on this machine. Required."},
				property{Name: "after", Type: "integer", Desc: "Put it after this paragraph (1-based)."},
				property{Name: "before", Type: "integer", Desc: "Put it before this paragraph."},
				property{Name: "at", Type: "string", Desc: "start or end (default end) when neither is given.", Enum: wordAtWhere},
			},
			Required: []string{"path"},
		},
		{
			Name: "set_page_setup",
			Desc: "Page size and margins of a section (default: every section): orientation, paper, margins in points, " +
				"header/footer distance, different first page. Needs WordApiDesktop 1.1 (Microsoft 365 desktop) — refused by name elsewhere.",
			Props: []property{
				property{Name: "section", Type: "integer", Desc: "Section number, 1-based. Omit for all sections."},
				property{Name: "orientation", Type: "string", Desc: "Portrait or Landscape.", Enum: wordOrientations},
				property{Name: "paper", Type: "string", Desc: "A4, A3, A5, B4, B5, Letter, Legal, Tabloid, Executive.", Enum: wordPapers},
				property{Name: "margins", Type: "object", Desc: "{left, right, top, bottom} in points (72 = 1 inch, 28.35 = 1 cm)."},
				property{Name: "header_distance", Type: "number", Desc: "Header from page edge, points."},
				property{Name: "footer_distance", Type: "number", Desc: "Footer from page edge, points."},
				property{Name: "different_first_page", Type: "boolean", Desc: "First page has its own header/footer."},
			},
		},
		{
			Name: "insert_content_control",
			Desc: "Wrap a paragraph (or `text` inside it) in a rich-text content control — a named, fillable slot with a tag " +
				"and title, optionally locked. The way to turn a document into a template or to mark a field to fill later.",
			Props: []property{
				property{Name: "paragraph", Type: "integer", Desc: "The paragraph, 1-based. Required.", Also: []string{"from"}},
				property{Name: "text", Type: "string", Desc: "Words inside the paragraph to wrap; omit to wrap the whole paragraph."},
				property{Name: "tag", Type: "string", Desc: "Machine name, e.g. \"customer\". Required."},
				property{Name: "title", Type: "string", Desc: "Shown label."},
				property{Name: "placeholder", Type: "string", Desc: "Text shown while empty."},
				property{Name: "appearance", Type: "string", Desc: "BoundingBox (default), Tags, Hidden.", Enum: wordCCAppearances},
				property{Name: "locked", Type: "boolean", Desc: "true = cannot be edited or deleted by the person."},
			},
			Required: []string{"paragraph", "tag"},
		},
		{
			Name: "set_content_control",
			Desc: "Fill or relabel a content control found by tag (or id): new text, title, tag, placeholder, lock. Only the " +
				"fields you pass change.",
			Props: []property{
				property{Name: "tag", Type: "string", Desc: "Tag of the control (first match)."},
				property{Name: "id", Type: "integer", Desc: "Id from read_content_controls, when tags repeat."},
				property{Name: "text", Type: "string", Desc: "Replace the text inside."},
				property{Name: "title", Type: "string"}, property{Name: "new_tag", Type: "string"}, property{Name: "placeholder", Type: "string"},
				property{Name: "locked", Type: "boolean"},
			},
		},
		{
			Name: "delete_content_control",
			Desc: "Remove a content control by tag or id — keep_content (default true) leaves its text in place; false removes the text too.",
			Props: []property{
				property{Name: "tag", Type: "string"}, property{Name: "id", Type: "integer"},
				property{Name: "keep_content", Type: "boolean", Desc: "Default true."},
			},
		},
		{
			Name: "set_style_format",
			Desc: "Change a paragraph STYLE itself — font, size, bold, italic, colour, alignment, spacing, indents — so every " +
				"paragraph in that style changes at once, now and later. `style` is a built-in name (Heading1, Normal, " +
				"ListParagraph — language-independent) or the name the document shows (describe_style). `create: true` makes a " +
				"new paragraph style of that name when none exists. Needs WordApi 1.5 — refused by name on 2019/2021.",
			Props: []property{
				property{Name: "style", Type: "string", Desc: "Built-in (Heading2) or the document's own style name. Required."},
				property{Name: "font", Type: "string", Desc: "Typeface, e.g. \"맑은 고딕\"."},
				property{Name: "size", Type: "number", Desc: "Points."},
				property{Name: "bold", Type: "boolean"},
				property{Name: "italic", Type: "boolean"},
				property{Name: "color", Type: "string", Desc: "#RRGGBB."},
				property{Name: "align", Type: "string", Desc: "Left, Centered, Right, Justified.", Enum: wordAligns},
				property{Name: "space_before", Type: "number", Desc: "Points before each paragraph."},
				property{Name: "space_after", Type: "number", Desc: "Points after each paragraph."},
				property{Name: "line_spacing", Type: "number", Desc: "Points between lines (12 ≈ single at 12pt)."},
				property{Name: "first_line_indent", Type: "number", Desc: "Points."},
				property{Name: "left_indent", Type: "number", Desc: "Points."},
				property{Name: "create", Type: "boolean", Desc: "Make the style when the document has none by that name (a new paragraph style)."},
			},
			Required: []string{"style"},
		},
		{
			Name: "insert_footnote",
			Desc: "Add a footnote (default) or an endnote at a place in a paragraph: after `text` inside it, or at the " +
				"paragraph's end when text is omitted. Needs WordApi 1.5 — refused by name on 2019/2021.",
			Props: []property{
				property{Name: "paragraph", Type: "integer", Desc: "The paragraph the note hangs on, 1-based. Required.", Also: []string{"from"}},
				property{Name: "text", Type: "string", Desc: "Word(s) inside the paragraph the mark goes after. Omit for the paragraph's end."},
				property{Name: "note", Type: "string", Desc: "The note itself. Required."},
				property{Name: "kind", Type: "string", Desc: "footnote (default) or endnote.", Enum: wordNoteKinds},
			},
			Required: []string{"paragraph", "note"},
		},
		{
			Name: "delete_footnote",
			Desc: "Remove one footnote or endnote by its number (from read_footnotes). The mark and the note go; the text stays.",
			Props: []property{
				property{Name: "number", Type: "integer", Desc: "1-based, in document order within its kind. Required."},
				property{Name: "kind", Type: "string", Desc: "footnote (default) or endnote.", Enum: wordNoteKinds},
			},
			Required: []string{"number"},
		},
		{
			Name: "set_header_footer",
			Desc: "Set the header or footer text of a section (default: first section, primary). Replaces what is " +
				"there. Word keeps page-number fields; to keep one, say so in `keep_fields`. To ADD a page number, use insert_field.",
			Props: []property{
				property{Name: "which", Type: "string", Desc: "header or footer. Required.", Enum: wordHeaderFooter},
				property{Name: "text", Type: "string", Desc: "The new text. Empty string clears it. Required."},
				property{Name: "section", Type: "integer", Desc: "Section number, 1-based (default 1)."},
				property{Name: "kind", Type: "string", Desc: "Primary (default), FirstPage, EvenPages.", Enum: wordHeaderKinds},
				property{Name: "align", Type: "string", Enum: wordAligns},
			},
			Required: []string{"which", "text"},
		},
		{
			Name: "set_hyperlink",
			Desc: "Put a hyperlink on a passage: the matches of `text` inside paragraphs from..to (or the whole " +
				"paragraphs when text is omitted). Omit url to remove the link.",
			Props:    withFromTo(property{Name: "text", Type: "string"}, property{Name: "url", Type: "string", Desc: "Omit to remove."}),
			Required: []string{"from"},
		},
		{
			Name: "replace_all",
			Desc: "Find and replace across the body (or from..to): every match of `find` becomes `replace`. " +
				"Case-insensitive unless match_case; whole-word optional. Answers how many changed and where. " +
				"Take a snapshot first if the words are common.",
			Props: withFromTo(
				property{Name: "find", Type: "string", Desc: "Required."}, property{Name: "replace", Type: "string", Desc: "Required (empty string deletes the matches)."},
				property{Name: "match_case", Type: "boolean"}, property{Name: "whole_word", Type: "boolean"},
				property{Name: "limit", Type: "integer", Desc: "Stop after this many (default: all)."}),
			Required: []string{"find", "replace"},
		},
		{
			Name: "add_comment",
			Desc: "Add a comment on a passage: the first match of `text` inside paragraphs from..to, or the whole " +
				"paragraph `from` when text is omitted. Needs WordApi 1.4 (Microsoft 365 / 2024).",
			Props:    withFromTo(property{Name: "text", Type: "string", Desc: "Anchor text inside the paragraphs."}, property{Name: "comment", Type: "string", Desc: "The comment. Required."}),
			Required: []string{"from", "comment"},
		},
		{
			Name:     "reply_comment",
			Desc:     "Reply to a comment thread by id (from read_comments). Needs WordApi 1.4.",
			Props:    []property{property{Name: "id", Type: "string", Desc: "Comment id. Required."}, property{Name: "text", Type: "string", Desc: "Reply. Required."}},
			Required: []string{"id", "text"},
		},
		{
			Name:     "resolve_comment",
			Desc:     "Mark a comment thread resolved (or reopen it), or delete it with delete:true. Needs WordApi 1.4.",
			Props:    []property{property{Name: "id", Type: "string", Desc: "Comment id. Required."}, property{Name: "resolved", Type: "boolean", Desc: "Default true."}, property{Name: "delete", Type: "boolean"}},
			Required: []string{"id"},
		},
		{
			Name:     "add_bookmark",
			Desc:     "Put a named bookmark on paragraphs from..to (a hidden anchor you or a hyperlink can jump to). Needs WordApi 1.4.",
			Props:    withFromTo(property{Name: "name", Type: "string", Desc: "Bookmark name — letters, digits, underscore; starts with a letter. Required."}),
			Required: []string{"from", "name"},
		},
		{
			Name:     "delete_bookmark",
			Desc:     "Remove a bookmark by name. The text stays. Needs WordApi 1.4.",
			Props:    []property{property{Name: "name", Type: "string", Desc: "Required."}},
			Required: []string{"name"},
		},
		{
			Name: "set_track_changes",
			Desc: "Turn change tracking Off, TrackAll, or TrackMineOnly. With tracking on, every edit these tools " +
				"make shows as a tracked change the person can accept or reject — the right mode when editing " +
				"someone else's document. Needs WordApi 1.4.",
			Props:    []property{property{Name: "mode", Type: "string", Desc: "Off, TrackAll, TrackMineOnly. Required.", Enum: wordTrackModes}},
			Required: []string{"mode"},
		},
		{
			Name:     "review_changes",
			Desc:     "Accept or reject tracked changes — all of them, or only those in paragraphs from..to. Needs WordApi 1.6.",
			Props:    withFromTo(property{Name: "what", Type: "string", Desc: "accept or reject. Required.", Enum: wordReviewWhats}),
			Required: []string{"what"},
		},
		{
			Name: "set_properties",
			Desc: "Document properties: title, subject, author, keywords, comments, category. Only the fields you " +
				"pass change.",
			Props: []property{
				property{Name: "title", Type: "string"}, property{Name: "subject", Type: "string"}, property{Name: "author", Type: "string"},
				property{Name: "keywords", Type: "string"}, property{Name: "comments", Type: "string"}, property{Name: "category", Type: "string"}},
		},
		{
			Name:     "restore_paragraphs",
			Desc:     "Put back a snapshot from snapshot_paragraphs, replacing the paragraphs at the snapshot's from..to as they are NOW. If paragraphs were inserted or deleted above it since, re-check the numbers first.",
			Props:    []property{property{Name: "snapshot", Type: "string", Desc: "Snapshot id from snapshot_paragraphs. Required."}},
			Required: []string{"snapshot"},
		},
		{
			Name: "set_tag",
			Desc: "Leave a note in the document for later turns (a custom document property under MAGI.*): a decision, " +
				"what is left. Values are capped at 255 characters by Word. Keys starting with MAGI.FIX. are " +
				"reserved for suggestions.",
			Props:    []property{property{Name: "key", Type: "string", Desc: "Required."}, property{Name: "value", Type: "string", Desc: "Required; empty string deletes the note."}},
			Required: []string{"key", "value"},
		},
		{
			Name: "suggest",
			Desc: "Propose a change WITHOUT making it — the person sees a card in the task pane and applies it with one " +
				"click. `fix` is the exact call to make: {tool, args}; only replace_paragraph, format_text, " +
				"format_paragraph, set_style, replace_all and insert_paragraphs can be applied from a card. Needs " +
				"WordApi 1.4 (settings).",
			Props: []property{
				property{Name: "what", Type: "string", Desc: "The suggestion, one sentence. Required."},
				property{Name: "why", Type: "string", Desc: "Why it would be better."},
				property{Name: "paragraph", Type: "integer", Desc: "Where it applies, 1-based."},
				property{Name: "fix", Type: "object", Desc: "{tool, args} — the call Apply should make."},
			},
			Required: []string{"what"},
		},
		{
			Name:     "drop_suggestion",
			Desc:     "Remove a pinned suggestion by key (from read_suggestions) without applying it. Needs WordApi 1.4.",
			Props:    []property{property{Name: "key", Type: "string", Desc: "Required."}},
			Required: []string{"key"},
		},
	}
}

var wordDocumentProp = property{
	Name: "document",
	Type: "string",
	Desc: "Omit it. This conversation is bound to one document and every call goes to that document. Only a hub-wide conversation (no document of its own) names one here, with a key from an earlier answer.",
	Also: []string{"doc"},
}
