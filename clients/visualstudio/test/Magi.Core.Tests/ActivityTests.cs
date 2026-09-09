using System.Text.Json;
using Xunit;

namespace Magi.Core.Tests;

public class ActivityTests
{
    private static BridgeReply Reply(string json) =>
        JsonSerializer.Deserialize<BridgeReply>(json, Wire.Json)!;

    /// <summary>
    /// A word this build has never seen is kept, not folded into "cannot say". A newer bridge
    /// saying something new is a different fact from a bridge that could not tell, and a screen
    /// showing an unfamiliar word is a better failure than one showing a familiar wrong one.
    /// </summary>
    [Fact]
    public void AWordFromANewerBridgeSurvives()
    {
        var a = Activity.Of(Reply("""{"id":1,"ok":true,"state":"compacting"}"""));
        Assert.Equal("compacting", a.State);
        Assert.Equal("compacting", a.Label());
    }

    /// <summary>
    /// "We could not ask" is not "it is idle". A reply that failed must not read as a companion
    /// sitting quietly — that is the exact confusion the vocabulary exists to prevent.
    /// </summary>
    [Fact]
    public void AFailedReplyIsUnknownRatherThanIdle()
    {
        var a = Activity.Of(Reply("""{"id":1,"ok":false,"error":"the bridge closed"}"""));
        Assert.Equal(ActivityState.Unknown, a.State);
        Assert.Equal("the bridge closed", a.Why);
        Assert.Equal("cannot say", a.Label());
    }

    /// <summary>Nothing at all is the same news as a failure, and must not be idle either.</summary>
    [Fact]
    public void NoReplyAtAllIsUnknown()
    {
        Assert.Equal(ActivityState.Unknown, Activity.Of(null).State);
    }

    /// <summary>
    /// An <c>unknown</c> the bridge said is not an <c>unknown</c> we concluded.
    /// </summary>
    /// <remarks>
    /// The bridge says the word itself when it could not ask the daemon — a socket path past the
    /// address limit, or a status round trip that failed — and it says it over a pipe that works.
    /// The caller that owns the child process acts on the difference: dropping a bridge that
    /// answered kills a healthy process, and for the path-length reason, which never changes, it
    /// would do so on every poll for the life of the window.
    /// </remarks>
    [Theory]
    [InlineData("""{"id":1,"ok":true,"state":"unknown","why":"socket path is too long"}""", true)]
    [InlineData("""{"id":1,"ok":true,"state":"attached"}""", true)]
    [InlineData("""{"id":1,"ok":false,"error":"the bridge closed"}""", false)]
    [InlineData("""{"id":1,"ok":true}""", false)]
    public void SayingUnknownIsNotTheSameAsNotAnswering(string json, bool answered)
    {
        Assert.Equal(answered, Activity.Of(Reply(json)).Answered);
    }

    /// <summary>What we build for ourselves has answered nothing, whatever else is filled in.</summary>
    [Fact]
    public void TheUnknownWeMakeOurselvesNeverCountsAsAnAnswer()
    {
        Assert.False(Activity.Unknown.Answered);
        Assert.False((Activity.Unknown with { Why = "magi is not installed" }).Answered);
        Assert.False(Activity.Of(null).Answered);
    }

    /// <summary>
    /// The bridge decides the word; this side only reads it. A reply with no state is a bridge that
    /// answered without answering, which is not something to guess about.
    /// </summary>
    [Fact]
    public void AnOkReplyWithNoStateIsStillUnknown()
    {
        Assert.Equal(ActivityState.Unknown, Activity.Of(Reply("""{"id":1,"ok":true}""")).State);
    }

    /// <summary>
    /// The sentence carries what it is on when there is something to say, and does not pad when
    /// there is not.
    /// </summary>
    [Theory]
    [InlineData("""{"id":1,"ok":true,"state":"working","doing":"running tests"}""", "working · running tests")]
    [InlineData("""{"id":1,"ok":true,"state":"working"}""", "working")]
    [InlineData("""{"id":1,"ok":true,"state":"waiting","asking":"run `rm -rf build`"}""",
                "waiting on you · run `rm -rf build`")]
    [InlineData("""{"id":1,"ok":true,"state":"not-running"}""", "not running")]
    // The state an ordinary running turn produces — the commonest reply on this wire, and the one
    // this client had no sentence for until 2026-09-10.
    [InlineData("""{"id":1,"ok":true,"state":"attached"}""", "attached")]
    public void TheSentenceIsBuiltFromWhatWasSaid(string json, string expected)
    {
        Assert.Equal(expected, Activity.Of(Reply(json)).Label());
    }

    /// <summary>
    /// A key the companion never filled must be absent rather than blank, so a screen merging a new
    /// reading over an old one does not overwrite what it knew with nothing. The model in
    /// particular is only filled when the request named a session.
    /// </summary>
    [Fact]
    public void SetupCarriesOnlyWhatWasSaid()
    {
        var a = Activity.Of(Reply("""{"id":1,"ok":true,"state":"attached","setup":{"permission":"ask"}}"""));
        Assert.Equal("ask", a.Setup["permission"]);
        Assert.False(a.Setup.ContainsKey("model"));
    }

    /// <summary>Whitespace is not a progress note, and must not pin the sentence on "working · ".</summary>
    [Fact]
    public void BlankPartsAreDroppedRatherThanShown()
    {
        var a = Activity.Of(Reply("""{"id":1,"ok":true,"state":"working","doing":"   "}"""));
        Assert.Null(a.Doing);
        Assert.Equal("working", a.Label());
    }
}
