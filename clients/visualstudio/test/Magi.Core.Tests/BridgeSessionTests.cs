using System.Text;
using System.Text.Json;
using Xunit;

namespace Magi.Core.Tests;

/// <summary>
/// A bridge made of streams, so the framing is testable without a binary or an IDE.
/// </summary>
internal sealed class FakeBridge : IDisposable
{
    private readonly AnonymousPipeServerStreamPair _toClient = new();
    private readonly MemoryStream _fromClient = new();
    private readonly StreamWriter _write;

    public BridgeSession Session { get; }

    /// <summary>Every line the client sent, as it sent it.</summary>
    public List<string> Sent { get; } = [];

    public FakeBridge()
    {
        _write = new StreamWriter(_toClient.Writer, new UTF8Encoding(false)) { AutoFlush = true };
        Session = new BridgeSession(new StreamReader(_toClient.Reader, Encoding.UTF8),
                                    new RecordingWriter(Sent));
    }

    /// <summary>Answer with one raw line, exactly as written.</summary>
    public void Say(string line) => _write.Write(line + "\n");

    public void Dispose() { Session.Dispose(); _write.Dispose(); _toClient.Dispose(); _fromClient.Dispose(); }
}

/// <summary>A writer that keeps whole lines, so a test can assert what actually crossed.</summary>
internal sealed class RecordingWriter(List<string> lines) : TextWriter
{
    private readonly StringBuilder _buffer = new();
    public override Encoding Encoding => Encoding.UTF8;

    public override void Write(char value)
    {
        if (value == '\n') { lock (lines) { lines.Add(_buffer.ToString()); } _buffer.Clear(); }
        else _buffer.Append(value);
    }
}

/// <summary>An in-memory pipe pair, because a pipe is the shape a child process's stdio has.</summary>
internal sealed class AnonymousPipeServerStreamPair : IDisposable
{
    private readonly System.IO.Pipes.AnonymousPipeServerStream _server = new(System.IO.Pipes.PipeDirection.Out);
    private readonly System.IO.Pipes.AnonymousPipeClientStream _client;

    public AnonymousPipeServerStreamPair()
    {
        _client = new System.IO.Pipes.AnonymousPipeClientStream(
            System.IO.Pipes.PipeDirection.In, _server.GetClientHandleAsString());
    }

    public Stream Writer => _server;
    public Stream Reader => _client;

    public void Dispose() { _server.Dispose(); _client.Dispose(); }
}

public class BridgeSessionTests
{
    /// <summary>
    /// One request per line is the whole framing. Anything that put a newline inside a request
    /// would be read by the bridge as the start of a second one, and the reply to that phantom
    /// would be handed to whoever asked next.
    /// </summary>
    [Fact]
    public async Task ARequestIsExactlyOneLine()
    {
        using var bridge = new FakeBridge();
        var asking = bridge.Session.AskAsync(new BridgeRequest
        {
            Method = "daemon",
            Req = JsonDocument.Parse("""{"method":"submit","text":"a\nb"}""").RootElement,
        }, TimeSpan.FromSeconds(5));
        bridge.Say("""{"id":1,"ok":true,"resp":{"ok":true}}""");
        await asking;

        Assert.Single(bridge.Sent);
        Assert.DoesNotContain('\n', bridge.Sent[0]);
        Assert.Contains(@"a\nb", bridge.Sent[0]);
    }

    /// <summary>
    /// Replies are matched by id, not by arrival order. A bridge that answers a slow request after
    /// a fast one would otherwise hand each caller the other's answer — a wrong answer, which is
    /// worse than a slow one.
    /// </summary>
    [Fact]
    public async Task RepliesGoToTheRequestTheyName()
    {
        using var bridge = new FakeBridge();
        var first = bridge.Session.AskAsync(new BridgeRequest { Method = "about" }, TimeSpan.FromSeconds(5));
        var second = bridge.Session.AskAsync(new BridgeRequest { Method = "activity" }, TimeSpan.FromSeconds(5));

        bridge.Say("""{"id":2,"ok":true,"state":"idle"}""");
        bridge.Say("""{"id":1,"ok":true,"version":"0.41.0"}""");

        Assert.Equal("idle", (await second).State);
        Assert.Equal("0.41.0", (await first).Version);
    }

    /// <summary>
    /// A line that is not JSON is not a reply to anything. Dropping it keeps every waiter matched;
    /// treating it as an answer would hand a caller garbage.
    /// </summary>
    [Fact]
    public async Task ALineThatIsNotJsonIsIgnoredRatherThanAnswered()
    {
        using var bridge = new FakeBridge();
        var asking = bridge.Session.AskAsync(new BridgeRequest { Method = "about" }, TimeSpan.FromSeconds(5));
        bridge.Say("this is not json");
        bridge.Say("");
        bridge.Say("""{"id":1,"ok":true,"version":"0.41.0"}""");
        Assert.Equal("0.41.0", (await asking).Version);
    }

    /// <summary>
    /// A bridge that goes away must settle everybody still waiting. A task nobody completes is a
    /// screen that says nothing for ever, which is the failure a person reports as "it hangs".
    /// </summary>
    [Fact]
    public async Task EverybodyWaitingIsAnsweredWhenTheBridgeGoesAway()
    {
        var bridge = new FakeBridge();
        var asking = bridge.Session.AskAsync(new BridgeRequest { Method = "about" }, TimeSpan.FromSeconds(30));
        bridge.Dispose();
        var reply = await asking.WaitAsync(TimeSpan.FromSeconds(5));
        Assert.False(reply.Ok);
        Assert.False(string.IsNullOrEmpty(reply.Error));
    }

    /// <summary>
    /// A line that answers nobody must not be handed to whoever asked last.
    /// </summary>
    /// <remarks>
    /// The bridge sends exactly this today: a malformed request has no id to answer to, so the
    /// reply carries none. It still has to reach somebody — a client that gets nothing back cannot
    /// tell a bad request from a bridge that died — but giving it to the pending <c>about</c> would
    /// answer a question with somebody else's news. Subscription frames will arrive down the same
    /// path when <c>watch</c> lands.
    /// </remarks>
    [Fact]
    public async Task ALineThatAnswersNobodyGoesToTheSideChannel()
    {
        using var bridge = new FakeBridge();
        var frames = new List<BridgeReply>();
        bridge.Session.Frame += frames.Add;

        var asking = bridge.Session.AskAsync(new BridgeRequest { Method = "about" }, TimeSpan.FromSeconds(5));
        bridge.Say("""{"ok":false,"error":"malformed request: unexpected end of JSON input"}""");
        bridge.Say("""{"id":1,"ok":true,"version":"0.41.0"}""");
        Assert.Equal("0.41.0", (await asking).Version);
        Assert.Single(frames);
        Assert.Contains("malformed", frames[0].Error);
    }

    /// <summary>
    /// A bridge that never answers must raise, not return a null a caller could read as "it said
    /// nothing". Those are different facts.
    /// </summary>
    [Fact]
    public async Task SilenceIsATimeoutAndNotAnEmptyAnswer()
    {
        using var bridge = new FakeBridge();
        await Assert.ThrowsAsync<TimeoutException>(() =>
            bridge.Session.AskAsync(new BridgeRequest { Method = "about" }, TimeSpan.FromMilliseconds(200)));
    }
}
