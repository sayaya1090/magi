namespace Magi.Core;

/// <summary>
/// What the companion is doing, as the bridge decided it.
/// </summary>
/// <remarks>
/// This type <b>does not decide anything</b>. Whether a companion that is blocked on a person reads
/// as working or waiting, whether a socket nobody answers is "not running" or "unknown", what a
/// blank progress note means — all of that is one rule with one right answer, and it lives in
/// <c>internal/adapter/idebridge/activity.go</c> because it was on its way to being written for the
/// third time in a third language. Here it is read, not re-derived.
/// </remarks>
public sealed record Activity(string State, string? Asking, string? Doing, string? Why,
                              IReadOnlyDictionary<string, string> Setup)
{
    /// <summary>Nobody answered. Use <see cref="Of"/> for a reading the bridge actually gave.</summary>
    public static readonly Activity Unknown =
        new(ActivityState.Unknown, null, null, null, new Dictionary<string, string>());

    /// <summary>
    /// Whether the bridge said this, or we concluded it because the bridge did not answer.
    /// </summary>
    /// <remarks>
    /// The state word cannot carry this. <c>unknown</c> is a word the bridge itself says — for a
    /// socket path past the address limit, or a status round trip that failed — and it says it over
    /// a working stdio pipe, having answered the question. That is a different fact from a bridge
    /// that timed out or whose pipe is shut, and a caller that folds the two treats a healthy
    /// bridge as a dead one. This repository's own rule, one layer up: "we could not ask" and "it
    /// answered" must not arrive as the same news.
    /// </remarks>
    public bool Answered { get; init; }

    /// <summary>
    /// Read one <c>activity</c> reply.
    /// </summary>
    /// <remarks>
    /// A word this build has never heard of is kept as it is rather than folded into
    /// <see cref="ActivityState.Unknown"/>. A newer bridge saying something new is not the same as
    /// a bridge that could not tell — and the screen showing an unfamiliar word is a better failure
    /// than one showing a familiar wrong one.
    /// </remarks>
    public static Activity Of(BridgeReply? reply)
    {
        if (reply is null || !reply.Ok || string.IsNullOrWhiteSpace(reply.State))
        {
            return Unknown with { Why = reply?.Error };
        }
        return new Activity(reply.State!, Blank(reply.Asking), Blank(reply.Doing), Blank(reply.Why),
                            reply.Setup ?? new Dictionary<string, string>()) { Answered = true };
    }

    private static string? Blank(string? s) => string.IsNullOrWhiteSpace(s) ? null : s;

    /// <summary>
    /// The one-line sentence a screen shows.
    /// </summary>
    /// <remarks>
    /// ⚠ Deliberately word-for-word the same as the VS Code client's <c>activity.label</c>. The
    /// bridge hands over the parts and not the sentence, so this IS a second copy — kept identical
    /// so the two cannot say different things about one state, and named here so that when the
    /// sentence moves into the bridge there is one place to delete.
    /// </remarks>
    public string Label() => State switch
    {
        ActivityState.NotRunning => "not running",
        ActivityState.Attached => "attached",
        ActivityState.Working => Doing is null ? "working" : $"working · {Doing}",
        ActivityState.Waiting => Asking is null ? "waiting on you" : $"waiting on you · {Asking}",
        ActivityState.Unknown => "cannot say",
        // A word from a newer bridge. Showing it beats showing "cannot say" about something it in
        // fact said.
        _ => State,
    };
}
