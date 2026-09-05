namespace Magi.Ppt.Hand;

/// <summary>표 다섯 도구. 표는 도형 id 를 지키는 것이 요점이다 — replace_table 만 id 가 바뀐다.</summary>
public sealed partial class Hand
{
    private static List<(int, int, int, int)>? Merges(Args a) => a.Has("merge") ? a.Objects("merge").Select(m => (m.Int("row") ?? throw new HandError("merge 에 row 가 없습니다"), m.Int("column") ?? throw new HandError("merge 에 column 이 없습니다"), Math.Max(1, m.Int("rows") ?? 1), Math.Max(1, m.Int("columns") ?? 1))).ToList() : null;
    private TableInfo TableOn(int n, string id)
    {
        ShapeOn(n, id);
        return ops.TableOf(n, id) ?? throw new HandError($"도형 {id} 은 표가 아닙니다 — 이 장의 표: {(ops.TablesOn(n).Count == 0 ? "없음" : string.Join(", ", ops.TablesOn(n).Select(t => t.ShapeId)))}");
    }

    private (Dictionary<string, object?>, List<string>)? Tables(string op, Args a)
    {
        switch (op)
        {
            case "add_table":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var rows = a.Int("rows") ?? throw new HandError("rows 가 없습니다"); var cols = a.Int("columns") ?? throw new HandError("columns 가 없습니다");
                if (rows < 1 || cols < 1) throw new HandError($"표는 1×1 이상입니다 — {rows}×{cols}");
                var values = a.Has("values") ? a.Rows("values") : null;
                if (values is not null && (values.Count > rows || values.Any(r => r.Count > cols))) throw new HandError($"values 가 표보다 큽니다 — 표는 {rows}×{cols}, values 는 {values.Count}줄·최대 {values.Max(r => r.Count)}칸");
                var before = ops.TablesOn(n).Count;
                var spec = new TableSpec(rows, cols, a.Num("left"), a.Num("top"), a.Num("width"), a.Num("height"), values, a.Str("font"), a.Num("size"), a.Bool("header_bold") ?? false, Hex(a, "borders", true), a.Str("table_style"),
                    a.Bool("header_row"), a.Bool("banded_rows"), a.Bool("banded_columns"), a.Bool("first_column"), a.Has("column_widths") ? a.Numbers("column_widths") : null, a.Has("row_heights") ? a.Numbers("row_heights") : null, Merges(a), OneOf(a, "valign", VAligns));
                var id = ops.AddTable(n, spec); Mutated();
                var dup = before > 0 ? $" · ⚠ 이 장에 표가 이미 {before}개 있었습니다 — 그 표를 바꾸려던 것이면 replace_table 이나 set_table_cells 입니다" : "";
                return (new() { ["slide"] = n, ["shape_id"] = id, ["rows"] = rows, ["columns"] = cols, ["tables_before"] = before }, new() { $"슬라이드 {n}: {rows}×{cols} 표 {id} 추가" + (spec.HeaderBold ? " (헤더 굵게)" : "") + dup });
            }
            case "replace_table":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id"));
                var tables = ops.TablesOn(n);
                TableInfo old;
                if (a.Str("shape_id") is string id) old = TableOn(n, id);
                else if (tables.Count == 1) old = tables[0];
                else throw new HandError(tables.Count == 0 ? $"슬라이드 {n} 에 표가 없습니다 — 새로 만들려면 add_table" : $"슬라이드 {n} 에 표가 {tables.Count}개라 어느 것인지 shape_id 로 말해 주세요: {string.Join(", ", tables.Select(t => t.ShapeId))}");
                var rows = a.Int("rows") ?? old.Rows; var cols = a.Int("columns") ?? old.Columns;
                var values = a.Has("values") ? a.Rows("values") : old.Cells.Take(rows).Select(r => r.Take(cols).ToList()).ToList();
                if (values.Count > rows || values.Any(r => r.Count > cols)) throw new HandError($"values 가 표보다 큽니다 — 표는 {rows}×{cols}");
                var spec = new TableSpec(rows, cols, a.Num("left") ?? old.Left, a.Num("top") ?? old.Top, a.Num("width") ?? old.Width, a.Num("height") ?? old.Height, values, a.Str("font"), a.Num("size"), a.Bool("header_bold") ?? false, Hex(a, "borders", true), a.Str("table_style"),
                    a.Bool("header_row"), a.Bool("banded_rows"), a.Bool("banded_columns"), a.Bool("first_column"), a.Has("column_widths") ? a.Numbers("column_widths") : null, a.Has("row_heights") ? a.Numbers("row_heights") : null, Merges(a), OneOf(a, "valign", VAligns));
                ops.DeleteShape(n, old.ShapeId); var made = ops.AddTable(n, spec); Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = made, ["replaced"] = old.ShapeId, ["rows"] = rows, ["columns"] = cols }, new() { $"슬라이드 {n}: 표 {old.ShapeId} 를 {rows}×{cols} 표 {made} 로 다시 지었습니다 — **id 가 바뀌었습니다**" });
            }
            case "set_table_cells":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var t = TableOn(n, id);
                var cells = a.Objects("cells").Select(c => (c.Int("row") ?? throw new HandError("cells 항목에 row 가 없습니다"), c.Int("column") ?? throw new HandError("cells 항목에 column 이 없습니다"), c.Str("text") ?? "")).ToList();
                if (cells.Count == 0) throw new HandError("cells 가 비었습니다 — [{row, column, text}]");
                foreach (var (r, c, _) in cells) if (r < 0 || r >= t.Rows || c < 0 || c >= t.Columns) throw new HandError($"칸 ({r},{c}) 이 표 밖입니다 — 이 표는 {t.Rows}×{t.Columns}(0부터)");
                ops.SetCells(n, id, cells); Mutated();
                return (new() { ["slide"] = n, ["shape_id"] = id, ["cells"] = cells.Count }, new() { $"슬라이드 {n} · 표 {id}: 칸 {cells.Count}개를 적었습니다" });
            }
            case "format_table_cells":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var t = TableOn(n, id);
                var given = new[] { a.Has("cells"), a.Has("row"), a.Has("column") }.Count(x => x);
                if (given != 1) throw new HandError("cells, row, column 중 정확히 하나만 주세요");
                List<(int, int)> cells;
                if (a.Has("row")) { var r = a.Int("row")!.Value; if (r < 0 || r >= t.Rows) throw new HandError($"줄 {r} 이 없습니다 — 이 표는 {t.Rows}줄(0부터)"); cells = Enumerable.Range(0, t.Columns).Select(c => (r, c)).ToList(); }
                else if (a.Has("column")) { var c = a.Int("column")!.Value; if (c < 0 || c >= t.Columns) throw new HandError($"칸 {c} 이 없습니다 — 이 표는 {t.Columns}칸(0부터)"); cells = Enumerable.Range(0, t.Rows).Select(r => (r, c)).ToList(); }
                else cells = a.Objects("cells").Select(c => (c.Int("row") ?? throw new HandError("cells 항목에 row 가 없습니다"), c.Int("column") ?? throw new HandError("cells 항목에 column 이 없습니다"))).ToList();
                var f = new CellFormat(Hex(a, "fill", true), Hex(a, "color"), a.Num("size"), a.Bool("bold"), a.Bool("italic"), OneOf(a, "align", new[] { "left", "center", "right", "justify" }, true), OneOf(a, "valign", VAligns), Hex(a, "borders", true), a.Num("border_weight"));
                if (f is { Fill: null, Color: null, Size: null, Bold: null, Italic: null, Align: null, VAlign: null, Borders: null }) throw new HandError("바꿀 서식이 하나도 안 왔습니다 — fill, color, size, bold, italic, align, valign, borders 중 하나는 주세요");
                ops.FormatCells(n, id, cells, f); Mutated();
                var where = a.Has("row") ? $"{a.Int("row")}번 줄" : a.Has("column") ? $"{a.Int("column")}번 칸(세로)" : $"칸 {cells.Count}개";
                return (new() { ["slide"] = n, ["shape_id"] = id, ["cells"] = cells.Count }, new() { $"슬라이드 {n} · 표 {id}: {where} 서식을 바꿨습니다" });
            }
            case "edit_table":
            {
                var n = ops.ResolveSlide(a.Int("slide"), a.Str("slide_id")); var id = a.Str("shape_id") ?? throw new HandError("shape_id 가 없습니다");
                var t = TableOn(n, id);
                var e = new TableEdit(a.Int("add_rows") ?? 0, a.Int("add_rows_at"), a.Int("add_columns") ?? 0, a.Int("add_columns_at"), a.Ints("delete_rows"), a.Ints("delete_columns"),
                    a.Has("column_widths") ? a.Numbers("column_widths") : null, a.Has("row_heights") ? a.Numbers("row_heights") : null, Merges(a), a.Str("table_style"), a.Bool("header_row"), a.Bool("banded_rows"), a.Bool("banded_columns"), a.Bool("first_column"));
                if (t.Rows - e.DeleteRows.Count + e.AddRows < 1 || t.Columns - e.DeleteColumns.Count + e.AddColumns < 1) throw new HandError("표가 비게 됩니다 — 표를 없애려면 delete_shape");
                var what = new List<string>();
                if (e.DeleteRows.Count > 0) what.Add($"줄 {string.Join(",", e.DeleteRows)} 삭제"); if (e.DeleteColumns.Count > 0) what.Add($"칸 {string.Join(",", e.DeleteColumns)} 삭제");
                if (e.AddRows > 0) what.Add($"줄 {e.AddRows}개 추가" + (e.AddRowsAt is int ra ? $"({ra}번에)" : "")); if (e.AddColumns > 0) what.Add($"칸 {e.AddColumns}개 추가" + (e.AddColumnsAt is int ca ? $"({ca}번에)" : ""));
                if (e.ColumnWidths is not null) what.Add("칸 너비"); if (e.RowHeights is not null) what.Add("줄 높이"); if (e.Merge is not null) what.Add($"병합 {e.Merge.Count}곳"); if (e.TableStyle is not null) what.Add("스타일 " + e.TableStyle);
                if (e.HeaderRow is not null || e.BandedRows is not null || e.BandedColumns is not null || e.FirstColumn is not null) what.Add("띠·머리글 옵션");
                if (what.Count == 0) throw new HandError("바꿀 것이 하나도 안 왔습니다");
                ops.EditTable(n, id, e); Mutated();
                var now = ops.TableOf(n, id)!;
                return (new() { ["slide"] = n, ["shape_id"] = id, ["rows"] = now.Rows, ["columns"] = now.Columns }, new() { $"슬라이드 {n} · 표 {id}: {string.Join(", ", what)} — 지금 {now.Rows}×{now.Columns}" });
            }
        }
        return null;
    }
}
