using System.Text.Json;
using System.Text.Json.Serialization;

namespace Magi.Core;

/// <summary>
/// The bridge's wire, as the core spells it (<c>internal/adapter/idebridge/bridge.go</c>).
/// </summary>
/// <remarks>
/// Field names are copied, not invented. <c>System.Text.Json</c> drops unknown keys silently and
/// hands back a default for missing ones — the same trap Kotlin's <c>ignoreUnknownKeys</c> and
/// TypeScript's <c>undefined</c> set — so a name that disagrees is not an exception but a default,
/// and the screen says "nothing" while nothing fails. <c>WireTests</c> reads the Go source and
/// checks these names against it.
/// <para>
/// Note what is NOT here: the workspace key, the socket path, the FNV constant. The bridge derives
/// them (<c>docs/IDE_BRIDGE</c>), and both existing ports got that derivation wrong the first time.
/// A copy here would be the third.
/// </para>
/// </remarks>
public static class Wire
{
    /// <summary>One JSON object per line, both directions. Never indented, never multi-line.</summary>
    public static readonly JsonSerializerOptions Json = new()
    {
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        WriteIndented = false,
    };

    /// <summary>
    /// UTF-8 with <b>no</b> byte-order mark. Never <c>Encoding.UTF8</c>, which emits one.
    /// </summary>
    /// <remarks>
    /// Measured 2026-09-09: three bytes of BOM in front of the first request and the bridge answers
    /// <c>malformed request: invalid character 'U+FEFF' looking for beginning of value</c> — and
    /// only for the first line, so a client would look like it fails its handshake and then works.
    /// The default <c>Encoding.UTF8</c> writes that BOM, so the safe spelling is the explicit one
    /// and <c>WireTests</c> checks no source file uses the other.
    /// </remarks>
    public static readonly System.Text.Encoding Utf8NoBom = new System.Text.UTF8Encoding(false);
}

/// <summary>What an editor writes to the bridge.</summary>
public sealed class BridgeRequest
{
    [JsonPropertyName("id")] public int Id { get; set; }
    [JsonPropertyName("method")] public string Method { get; set; } = "";

    /// <summary>
    /// The daemon request, forwarded byte for byte. Raw on purpose: anything this build's types do
    /// not name must still cross, which is the whole point of the forwarding door.
    /// </summary>
    [JsonPropertyName("req")] public JsonElement? Req { get; set; }

    /// <summary>
    /// Which conversation a question is about. Its absence is not a default — the daemon fills the
    /// model only when a request names a session, so an answer with no model means "nobody said
    /// which conversation", not "no model".
    /// </summary>
    [JsonPropertyName("session")] public string? Session { get; set; }
}

/// <summary>One line back from the bridge: a reply to an id, or a subscription frame.</summary>
public sealed class BridgeReply
{
    [JsonPropertyName("id")] public int Id { get; set; }
    [JsonPropertyName("ok")] public bool Ok { get; set; }
    [JsonPropertyName("error")] public string? Error { get; set; }

    // No `sub` here. The contract reserves it for subscription frames, but this build of the bridge
    // answers `about`, `activity` and `daemon` and nothing streams yet — and a field for a door
    // that does not exist is a claim that it does. It arrives with `watch`.

    // about
    [JsonPropertyName("version")] public string? Version { get; set; }
    [JsonPropertyName("methods")] public string[]? Methods { get; set; }
    [JsonPropertyName("workspace")] public string? Workspace { get; set; }
    [JsonPropertyName("socket")] public string? Socket { get; set; }

    /// <summary>
    /// Null when no companion could be asked — which is a different fact from one that advertised
    /// nothing. <see cref="Why"/> says which.
    /// </summary>
    [JsonPropertyName("daemon")] public DaemonInfo? Daemon { get; set; }

    /// <summary>Why the answer is the shape it is: no socket, nobody listening, a path too long.</summary>
    [JsonPropertyName("why")] public string? Why { get; set; }

    // activity
    [JsonPropertyName("state")] public string? State { get; set; }
    [JsonPropertyName("asking")] public string? Asking { get; set; }
    [JsonPropertyName("doing")] public string? Doing { get; set; }
    [JsonPropertyName("setup")] public Dictionary<string, string>? Setup { get; set; }

    // daemon
    [JsonPropertyName("resp")] public JsonElement? Resp { get; set; }
}

/// <summary>What the companion advertised when the bridge shook hands with it.</summary>
public sealed class DaemonInfo
{
    [JsonPropertyName("version")] public string? Version { get; set; }
    [JsonPropertyName("proto")] public int Proto { get; set; }

    /// <summary>
    /// The doors it says it has. Empty and null mean different things: an empty list is a peer that
    /// named none, null is a peer that was never asked.
    /// </summary>
    [JsonPropertyName("caps")] public string[]? Caps { get; set; }
}

/// <summary>
/// The one word for what the companion is doing, spelled the way the bridge spells it.
/// </summary>
/// <remarks>
/// Constants rather than an enum parsed from the wire: an unrecognised word from a newer bridge
/// must stay readable rather than becoming whichever enum member happens to be zero. Compare with
/// <see cref="Activity.Of"/>, which keeps the unknown word instead of dropping it.
/// </remarks>
public static class ActivityState
{
    public const string NotRunning = "not-running";

    /// <summary>
    /// It answered, and said no more than that — <b>not</b> "it is idle".
    /// </summary>
    /// <remarks>
    /// The bridge lands here when a status reply carries neither a question nor a progress note,
    /// and that is what an ordinary running turn looks like: the note is written by one builtin
    /// tool out of fifty, and the status door has no field meaning "a turn is running" at all. The
    /// reasoning is in <c>internal/adapter/idebridge/activity.go</c>, where the word is decided;
    /// this is a copy of the spelling and nothing else.
    /// <para>
    /// ⚠ This client spelled it <c>idle</c> until 2026-09-10 — a word the bridge had stopped saying
    /// nine hours after this file was written. The constant then matched no reply the bridge could
    /// send, so <see cref="Activity.Label"/> fell through to its unrecognised-word default and the
    /// panel drew the raw wire word. Nothing failed, which is the shape of every trap in this file.
    /// </para>
    /// </remarks>
    public const string Attached = "attached";

    public const string Working = "working";
    public const string Waiting = "waiting";
    public const string Unknown = "unknown";
}
