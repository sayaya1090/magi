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
        var unknown = hand.Handle(Call("add_chart", "{}"));
        Assert.Contains("아직 모릅니다", unknown.Error);
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
}
