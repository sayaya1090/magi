using System.Text.Json;
using Magi.Ppt.Hand;
using Xunit;

// 손의 규약을 PowerPoint 없이 잰다: SSE 파싱, 봉투 모양, 덱 동작의 답과 거절문. COM 층(InteropOps)은
// Windows 에서만 돌므로 여기서는 FakeOps 가 그 자리를 맡는다 — 규약이 같으면 둘 다 같은 Hand 를 쓴다.
public class HandTests
{
    private static HandCall Call(string op, string argsJson = "{}") =>
        new("c1", op, JsonSerializer.Deserialize<Dictionary<string, JsonElement>>(argsJson, Json.Options));

    [Fact]
    public void SseParsesEventAndMultilineData()
    {
        var frames = Sse.Parse(new[] { ": ping", "event: call", "data: {\"id\":\"1\",", "data: \"op\":\"x\"}", "", "data: tail" }).ToList();
        Assert.Equal(2, frames.Count);
        Assert.Equal("call", frames[0].Event);
        Assert.Equal("{\"id\":\"1\",\n\"op\":\"x\"}", frames[0].Data);
        Assert.Equal("message", frames[1].Event);
    }

    [Fact]
    public void ReplyCarriesDocumentEpochAndCount()
    {
        var hand = new Hand(new FakeOps("fake-1", "q.pptx"), 42);
        var r = hand.Handle(Call("list_slides"));
        Assert.Equal("c1", r.Id); Assert.Equal("fake-1", r.Document); Assert.Equal(42, r.Epoch); Assert.Null(r.Error);
        Assert.Equal(0, r.Count);
        hand.Handle(Call("add_slide", "{\"title\":\"요약\",\"body\":\"본문\"}"));
        Assert.Equal(1, hand.Handle(Call("list_slides")).Count); // 바꾼 뒤에만 오른다
    }

    [Fact]
    public void AddSlidesFillsPlaceholdersAndWarnsOnLongTitles()
    {
        var hand = new Hand(new FakeOps(), 1);
        var r = hand.Handle(Call("add_slides", "{\"slides\":[{\"title\":\"짧은 제목\",\"body\":\"a\\nb\"},{\"title\":\"이 제목은 열여덟 자를 넘겨서 두 줄로 접힐 것이다\",\"layout\":\"제목만\"}]}"));
        Assert.Null(r.Error);
        Assert.Equal(2, r.Result!["made"]);
        var rows = (List<Dictionary<string, object?>>)r.Result["slides"]!;
        Assert.Empty((List<string>)rows[0]["notes"]!);
        Assert.Contains("제목이 2줄로", ((List<string>)rows[1]["notes"]!)[0]);
        Assert.Contains("장 2개를 만들었습니다", r.Changed![0]);
        var read = hand.Handle(Call("read_slide", "{\"slide\":2}"));
        var shapes = (List<Dictionary<string, object?>>)read.Result!["shapes"]!;
        Assert.Equal("짧은 제목", shapes.First(s => (string?)s["placeholder"] == "Title")["text"]);
    }

    [Fact]
    public void SetTextFindsThePlaceholderAndReportsBeforeAfter()
    {
        var hand = new Hand(new FakeOps(), 1);
        hand.Handle(Call("add_slide", "{\"title\":\"전\",\"body\":\"b\"}"));
        var r = hand.Handle(Call("set_text", "{\"slide\":2,\"placeholder\":\"title\",\"text\":\"후\"}"));
        Assert.Null(r.Error);
        Assert.Contains("\"전\" → \"후\"", r.Changed![0]);
        var miss = hand.Handle(Call("set_text", "{\"slide\":2,\"placeholder\":\"subtitle\",\"text\":\"x\"}"));
        Assert.Contains("'subtitle' 자리가 없습니다", miss.Error);
    }

    [Fact]
    public void RefusalsNameTheDeckState()
    {
        var hand = new Hand(new FakeOps(), 1);
        Assert.Contains("이 덱은 지금 1장입니다", hand.Handle(Call("read_slide", "{\"slide\":9}")).Error);
        Assert.Contains("마지막 장은 지울 수 없습니다", hand.Handle(Call("delete_slide", "{\"slide\":1}")).Error);
        Assert.Contains("slide 나 slide_id 로 정확히", hand.Handle(Call("delete_slide")).Error);
        var unknown = hand.Handle(Call("fly", "{}"));
        Assert.Contains("모릅니다", unknown.Error);
        Assert.Contains("list_slides", unknown.Error);
    }

    [Fact]
    public void RenderReportsSizeAndBytes()
    {
        var r = new Hand(new FakeOps(), 1).Handle(Call("render_slide", "{\"slide\":1,\"max_width\":640}"));
        Assert.Null(r.Error);
        Assert.Equal("image/png", r.Result!["image_mime"]);
        Assert.Equal(640, r.Result["max_width"]);
        Assert.Contains("640×360", r.Changed![0]);
    }

    [Fact]
    public void ArgsAreLenientAboutNumberStrings()
    {
        var a = new Args(JsonSerializer.Deserialize<Dictionary<string, JsonElement>>("{\"slide\":\"3\",\"bold\":\"true\",\"w\":1.5}", Json.Options));
        Assert.Equal(3, a.Int("slide")); Assert.True(a.Bool("bold")); Assert.Equal(1.5, a.Num("w")); Assert.Null(a.Str("nope"));
    }

    [Fact]
    public void ReplyUsesTheHelpersDocumentKeyNotTheHandsOwn()
    {
        // 실측 2026-09-05: 손의 제 키("fake-deck")로 답하니 헬퍼가 404 「no attached deck called "fake-deck"」로 길을 못 찾았다.
        var hand = new Hand(new FakeOps("fake-deck", "l"), 1, "pid-fake-deck");
        Assert.Equal("pid-fake-deck", hand.Handle(Call("list_slides")).Document);
    }

    [Fact]
    public void ReplySerializesLikeTheHelperExpects()
    {
        var r = new Hand(new FakeOps("k", "l"), 7).Handle(Call("list_slides"));
        var json = JsonSerializer.Serialize(r, Json.Options);
        Assert.Contains("\"id\":\"c1\"", json); Assert.Contains("\"document\":\"k\"", json); Assert.Contains("\"epoch\":7", json);
        Assert.DoesNotContain("\"error\"", json);
    }

    // ── 도구 48개 — 헬퍼 catalogue 와 같은 이름들. 하나라도 빠지면 그 도구는 「모른다」로 거절된다. ──
    [Fact]
    public void KnowsEveryTool()
    {
        Assert.Equal(48, Hand.Known.Count);
        var hand = new Hand(new FakeOps(), 1);
        foreach (var op in Hand.Known)
        {
            var r = hand.Handle(Call(op, "{}"));
            Assert.False(r.Error?.Contains("모릅니다") == true, $"{op}: {r.Error}");
        }
    }

    /// <summary>
    /// 줄바꿈이 **진짜 문단**이 되는가.
    ///
    /// 눈으로는 안 갈린다 — 글머리 자리표시자는 소프트 줄바꿈(\n)에도 기호를 붙인다. 그런데 문단이
    /// 아니면 문단 단위로 할 수 있는 일이 전부 막힌다. 2021 실측에서 그 화면을 봤다(2026-09-05):
    /// 세 줄짜리 본문에 paragraphs:"each" 를 걸었는데 걸음이 하나였다.
    ///
    /// 여기서 재는 것은 **\r\n 을 걸러내는 것**이다. 그냥 \n → \r 만 하면 윈도우에서 온 글의 \r\n 이
    /// \r\r 이 되고, 줄 사이마다 **빈 문단이 하나씩** 끼어 애니메이션 걸음이 두 배가 된다.
    ///
    /// 안 재는 것: 이 함수를 **부르는 자리**들. COM 을 타므로 실물 PowerPoint 에서만 갈린다(§5.5).
    /// </summary>
    [Fact]
    public void TurnsLineBreaksIntoRealParagraphs()
    {
        Assert.Equal("가\r나\r다", InteropOps.AsParagraphs("가\n나\n다"));
        Assert.Equal("가\r나", InteropOps.AsParagraphs("가\r\n나"));   // 빈 문단이 끼면 안 된다
        Assert.Equal("가\r나", InteropOps.AsParagraphs("가\r나"));      // 이미 문단이면 그대로
        Assert.Equal("", InteropOps.AsParagraphs(null));
    }

    /// <summary>
    /// 답의 **칸 이름**이 365 판과 같은가.
    ///
    /// 이름의 차집합은 <c>hand_com_parity_test.go</c> 가 잰다 — 도구가 하나 빠지면 잡힌다. 그런데
    /// **답의 칸 이름은 아무도 안 쟀다.** 2021 실측에서 그 대가를 봤다(2026-09-05):
    /// <c>read_notes</c> 가 365 에서는 <c>notes</c>, 이 손에서는 <c>text</c> 였다. 노트는 잘 적혔는데
    /// 읽는 쪽이 「없다」로 읽었다 — 도구는 맞고 계약이 갈린 것이다.
    ///
    /// 손이 둘이면 계약도 둘이 되고, 그 갈라짐은 **양쪽을 다 돌려 봐야만** 보인다. 여기가 그것을
    /// 한쪽에서 잡는 자리다. 값은 안 본다 — 이름만 본다.
    /// </summary>
    [Fact]
    public void AnswersCarryTheSameFieldNamesAsThe365Hand()
    {
        var hand = new Hand(new FakeOps(), 1);
        // 365 판(OfficeHand.js)이 싣는 칸들. 여기 적힌 것이 하나라도 빠지면 모델이 한쪽에서
        // 배운 것이 다른 쪽에서 안 맞는다.
        var must = new (string Op, string Args, string[] Fields)[]
        {
            ("read_notes", "{\"slide\":1}", new[] { "has_notes", "notes" }),
            ("read_tags", "{\"slide\":1}", new[] { "tags" }),
            ("read_animation", "{\"slide\":1}", new[] { "has_animation", "steps" }),
            ("read_suggestions", "{}", new[] { "suggestions", "count" }),
            ("list_slides", "{}", new[] { "slides" }),
            ("read_slide", "{\"slide\":1}", new[] { "shapes" }),
        };
        foreach (var (op, args, fields) in must)
        {
            var r = hand.Handle(Call(op, args));
            Assert.True(r.Error is null, $"{op}: {r.Error}");
            foreach (var f in fields)
                Assert.True(r.Result!.ContainsKey(f), $"{op} 의 답에 '{f}' 칸이 없다 — 365 판은 그 이름으로 싣는다");
        }
    }

    [Fact]
    public void AlignsAndDistributesFromTheShapesOwnBox()
    {
        var a = new ShapeInfo("1", "a", "AutoShape", null, "", 10, 50, 100, 20);
        var b = new ShapeInfo("2", "b", "AutoShape", null, "", 200, 80, 50, 20);
        var c = new ShapeInfo("3", "c", "AutoShape", null, "", 400, 60, 100, 20);
        var left = Hand.Aligned(new[] { a, b, c }, "left");
        Assert.All(left, p => Assert.Equal(10, p.Left));
        var middle = Hand.Aligned(new[] { a, b, c }, "middle");
        Assert.All(middle, p => Assert.Equal(65, p.Top)); // (50+100)/2 - 10
        var dist = Hand.Aligned(new[] { a, b, c }, "distribute_h");
        Assert.Equal(10, dist[0].Left); Assert.Equal(400, dist[2].Left); Assert.Equal(230, dist[1].Left); // 빈 폭 240(490-250) 을 두 틈에 120씩
        var hand = new Hand(new FakeOps(), 1);
        hand.Handle(Call("add_shape", "{\"slide\":1,\"left\":10,\"top\":50,\"width\":100,\"height\":20}"));
        hand.Handle(Call("add_shape", "{\"slide\":1,\"left\":200,\"top\":80,\"width\":50,\"height\":20}"));
        var r = hand.Handle(Call("align_shapes", "{\"slide\":1,\"how\":\"top\",\"shape_ids\":[\"3\",\"4\"]}"));
        Assert.Null(r.Error); Assert.Equal(1, r.Result!["moved"]);
        Assert.Contains("distribute_h 는 도형이 3개 이상", hand.Handle(Call("align_shapes", "{\"slide\":1,\"how\":\"distribute_h\",\"shape_ids\":[\"3\",\"4\"]}")).Error);
        Assert.Contains("도형 99 이 없습니다", hand.Handle(Call("align_shapes", "{\"slide\":1,\"how\":\"left\",\"shape_ids\":[\"3\",\"99\"]}")).Error);
    }

    [Fact]
    public void TablesKeepTheirIdExceptWhenReplaced()
    {
        var hand = new Hand(new FakeOps(), 1);
        var made = hand.Handle(Call("add_table", "{\"slide\":1,\"rows\":2,\"columns\":3,\"values\":[[\"a\",\"b\",\"c\"]],\"header_bold\":true}"));
        Assert.Null(made.Error); var id = (string)made.Result!["shape_id"]!;
        Assert.Contains("2×3 표", made.Changed![0]); Assert.Contains("헤더 굵게", made.Changed[0]);
        Assert.Null(hand.Handle(Call("set_table_cells", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"cells\":[{{\"row\":1,\"column\":2,\"text\":\"z\"}}]}}")).Error);
        Assert.Contains("표 밖입니다", hand.Handle(Call("set_table_cells", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"cells\":[{{\"row\":5,\"column\":0,\"text\":\"z\"}}]}}")).Error);
        Assert.Contains("정확히 하나만", hand.Handle(Call("format_table_cells", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"row\":0,\"column\":1,\"bold\":true}}")).Error);
        Assert.Contains("0번 줄", hand.Handle(Call("format_table_cells", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"row\":0,\"bold\":true}}")).Changed![0]);
        var edited = hand.Handle(Call("edit_table", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"add_rows\":2,\"delete_columns\":[0]}}"));
        Assert.Null(edited.Error); Assert.Equal(4, edited.Result!["rows"]); Assert.Equal(2, edited.Result["columns"]);
        var dup = hand.Handle(Call("add_table", "{\"slide\":1,\"rows\":1,\"columns\":1}"));
        Assert.Contains("표가 이미 1개", dup.Changed![0]);
        Assert.Contains("표가 2개라 어느 것인지", hand.Handle(Call("replace_table", "{\"slide\":1,\"rows\":1,\"columns\":1}")).Error);
        var rep = hand.Handle(Call("replace_table", $"{{\"slide\":1,\"shape_id\":\"{id}\",\"rows\":1,\"columns\":2}}"));
        Assert.Null(rep.Error); Assert.NotEqual(id, rep.Result!["shape_id"]); Assert.Contains("id 가 바뀌었습니다", rep.Changed![0]);
        Assert.Contains("아는 표 스타일이 아닙니다", hand.Handle(Call("add_table", "{\"slide\":1,\"rows\":1,\"columns\":1,\"table_style\":\"Fancy\"}")).Error);
    }

    [Fact]
    public void SuggestionsUseTheSharedTagCodec()
    {
        var hand = new Hand(new FakeOps(), 1);
        var s = hand.Handle(Call("suggest", "{\"slide\":1,\"what\":\"제목을 줄이세요\",\"why\":\"두 줄로 접힙니다\",\"fix\":{\"tool\":\"set_text\",\"args\":{\"placeholder\":\"title\",\"text\":\"짧게\"}}}"));
        Assert.Null(s.Error); var key = (string)s.Result!["suggestion"]!;
        Assert.StartsWith("MAGI.FIX.", key); Assert.Contains("아직 안 고친 것", s.Changed![0]);
        Assert.Contains("누를 수 있는 손이 아닙니다", hand.Handle(Call("suggest", "{\"slide\":1,\"what\":\"x\",\"fix\":{\"tool\":\"delete_slide\"}}")).Error);
        var read = hand.Handle(Call("read_suggestions", "{}"));
        Assert.Equal(1, read.Result!["count"]);
        var row = ((List<Dictionary<string, object?>>)read.Result["suggestions"]!)[0];
        Assert.Equal("제목을 줄이세요", row["what"]); Assert.Equal(false, row["broken"]); Assert.NotNull(row["fix"]);
        Assert.Contains("제안의 키가 아닙니다", hand.Handle(Call("drop_suggestion", "{\"slide\":1,\"key\":\"NOTE\"}")).Error);
        Assert.Null(hand.Handle(Call("drop_suggestion", $"{{\"slide\":1,\"key\":\"{key}\"}}")).Error);
        Assert.Equal(0, hand.Handle(Call("read_suggestions", "{}")).Result!["count"]);
        // set_tag 는 대문자로 저장되고 답이 그 이름을 돌려준다
        var t = hand.Handle(Call("set_tag", "{\"slide\":1,\"key\":\"made.by\",\"value\":\"magi\"}"));
        Assert.Equal("MADE.BY", t.Result!["key"]); Assert.Contains("바꿔 저장했습니다", t.Changed![0]);
        Assert.Contains("MADE.BY", hand.Handle(Call("read_tags", "{\"slide\":1}")).Changed![0] + JsonSerializer.Serialize(hand.Handle(Call("read_tags", "{\"slide\":1}")).Result));
    }

    [Fact]
    public void AnimationReplacesAndCountsClicks()
    {
        var hand = new Hand(new FakeOps(), 1);
        var r = hand.Handle(Call("animate_slide", "{\"slide\":1,\"steps\":[{\"shape_id\":\"1\"},{\"shape_id\":\"2\",\"effect\":\"wipe\",\"start\":\"after_previous\",\"paragraphs\":\"each\"}]}"));
        Assert.Null(r.Error); Assert.Equal(2, r.Result!["steps"]); Assert.Equal(1, r.Result["clicks"]);
        var read = hand.Handle(Call("read_animation", "{\"slide\":1}"));
        Assert.Equal(true, read.Result!["has_animation"]); Assert.Equal(true, read.Result["all_known"]);
        Assert.Contains("effect 는 appear, fade, wipe, zoom", hand.Handle(Call("animate_slide", "{\"slide\":1,\"steps\":[{\"shape_id\":\"1\",\"effect\":\"fly\"}]}")).Error);
        var cleared = hand.Handle(Call("animate_slide", "{\"slide\":1,\"steps\":[]}"));
        Assert.Contains("전부 지웠습니다(2개)", cleared.Changed![0]);
    }

    [Fact]
    public void SnapshotRestoresWithANewId()
    {
        var hand = new Hand(new FakeOps(), 1);
        hand.Handle(Call("set_text", "{\"slide\":1,\"placeholder\":\"title\",\"text\":\"원본\"}"));
        var snap = hand.Handle(Call("snapshot_slide", "{\"slide\":1}"));
        Assert.Null(snap.Error); var id = (string)snap.Result!["snapshot"]!; var oldId = (string)snap.Result["slide_id"]!;
        hand.Handle(Call("set_text", "{\"slide\":1,\"placeholder\":\"title\",\"text\":\"망침\"}"));
        var back = hand.Handle(Call("restore_slide", $"{{\"slide\":1,\"snapshot\":\"{id}\"}}"));
        Assert.Null(back.Error); Assert.NotEqual(oldId, back.Result!["slide_id"]); Assert.Contains("id 가", back.Changed![0]);
        var shapes = (List<Dictionary<string, object?>>)hand.Handle(Call("read_slide", "{\"slide\":1}")).Result!["shapes"]!;
        Assert.Equal("원본", shapes.First(s => (string?)s["placeholder"] == "CenterTitle")["text"]);
        Assert.Contains("그런 스냅숏이 없습니다", hand.Handle(Call("restore_slide", "{\"snapshot\":\"snap-9\"}")).Error);
    }

    [Fact]
    public void ChartsRefuseMismatchedSeriesAndNameTheKind()
    {
        var hand = new Hand(new FakeOps(), 1);
        Assert.Contains("값이 2개인데 항목은 3개", hand.Handle(Call("add_chart", "{\"slide\":1,\"categories\":[\"a\",\"b\",\"c\"],\"series\":[{\"name\":\"s\",\"values\":[1,2]}]}")).Error);
        Assert.Contains("모르는 차트 종류", hand.Handle(Call("add_chart", "{\"slide\":1,\"kind\":\"donut\",\"categories\":[\"a\"],\"series\":[{\"values\":[1]}]}")).Error);
        var r = hand.Handle(Call("add_chart", "{\"slide\":1,\"kind\":\"꺾은선\",\"categories\":[\"a\",\"b\"],\"series\":[{\"name\":\"s\",\"values\":[1,2]}],\"new_slide\":true}"));
        Assert.Null(r.Error); Assert.Equal("꺾은선", r.Result!["chart"]); Assert.Equal(2, r.Result["slide"]); Assert.Equal(true, r.Result["data_sheet"]);
        Assert.Contains("번호는 전부 하나씩 밀렸습니다", r.Changed![1]);
    }

    [Fact]
    public void FormatShapeSaysWhatItChangedAndRefusesEmptyOrBadValues()
    {
        var hand = new Hand(new FakeOps(), 1);
        var r = hand.Handle(Call("format_shape", "{\"slide\":1,\"shape_id\":\"1\",\"bold\":true,\"color\":\"#1F4E79\",\"align\":\"center\"}"));
        Assert.Null(r.Error); Assert.Equal(3, r.Result!["changed"]); Assert.Contains("굵게 → True", r.Changed![0]); Assert.Contains("글자색 → #1F4E79", r.Changed[0]);
        Assert.Contains("바꿀 것이 하나도", hand.Handle(Call("format_shape", "{\"slide\":1,\"shape_id\":\"1\"}")).Error);
        Assert.Contains("#RRGGBB", hand.Handle(Call("format_shape", "{\"slide\":1,\"shape_id\":\"1\",\"color\":\"blue\"}")).Error);
        Assert.Contains("underline 는", hand.Handle(Call("format_shape", "{\"slide\":1,\"shape_id\":\"1\",\"underline\":\"Squiggle\"}")).Error);
        Assert.Contains("decorative 는 이 손", hand.Handle(Call("format_shape", "{\"slide\":1,\"shape_id\":\"1\",\"decorative\":true}")).Error);
        var ft = hand.Handle(Call("set_text", "{\"slide\":1,\"placeholder\":\"title\",\"text\":\"매출 140억 달성\"}"));
        var run = hand.Handle(Call("format_text", "{\"slide\":1,\"shape_id\":\"1\",\"find\":\"140억\",\"color\":\"#FF0000\"}"));
        Assert.Null(run.Error); Assert.Equal(3, run.Result!["start"]); Assert.Equal(4, run.Result["length"]);
        Assert.Contains("'없음' 가 없습니다", hand.Handle(Call("format_text", "{\"slide\":1,\"shape_id\":\"1\",\"find\":\"없음\",\"bold\":true}")).Error);
    }

    [Fact]
    public void ThemeColorsWarnWhenTheChangeIsInvisible()
    {
        var hand = new Hand(new FakeOps(), 1);
        var r = hand.Handle(Call("set_theme_colors", "{\"slide\":1,\"scope\":\"master\",\"colors\":{\"accent1\":\"#4472C5\",\"light1\":\"#F3F1EC\"}}"));
        Assert.Null(r.Error); Assert.Equal(2, r.Result!["set"]);
        Assert.Contains(r.Changed!, l => l.Contains("거의 같은 색") && l.Contains("accent1") && !l.Contains("light1"));
        Assert.Contains("테마 색 이름이 아닙니다", hand.Handle(Call("set_theme_colors", "{\"slide\":1,\"colors\":{\"accent9\":\"#000000\"}}")).Error);
        var read = hand.Handle(Call("read_theme_colors", "{\"slide\":1}"));
        Assert.Equal("#F3F1EC", ((Dictionary<string, object?>)read.Result!["theme"]!)["light1"]);
        Assert.Contains("테마 배경으로 되돌렸습니다", hand.Handle(Call("set_background", "{\"slide\":1}")).Changed![0]);
        Assert.Contains("kind=picture 에는 path", hand.Handle(Call("set_background", "{\"slide\":1,\"kind\":\"picture\"}")).Error);
    }

    [Fact]
    public void FindShapesFiltersAcrossTheDeck()
    {
        var hand = new Hand(new FakeOps(), 1);
        hand.Handle(Call("add_slides", "{\"slides\":[{\"title\":\"매출\",\"body\":\"성장\"},{\"title\":\"비용\"}]}"));
        var r = hand.Handle(Call("find_shapes", "{\"text\":\"매출\"}"));
        Assert.Null(r.Error); Assert.Equal(1, r.Result!["matched"]);
        var titles = hand.Handle(Call("find_shapes", "{\"placeholder\":\"title\"}"));
        Assert.Equal(3, titles.Result!["matched"]);
        var one = hand.Handle(Call("find_shapes", "{\"slide\":3,\"placeholder\":\"title\"}"));
        Assert.Equal(1, one.Result!["matched"]);
        Assert.Equal(0, hand.Handle(Call("advise", "{\"items\":[{\"message\":\"m\",\"why\":\"w\"}]}")).Changed!.Count); // 안내는 한 일이 아니다
    }
}
