using System.Reflection;
using System.Text.Json.Serialization;
using Xunit;

namespace Magi.Core.Tests;

/// <summary>
/// The wire names are checked against the Go source that writes them.
/// </summary>
/// <remarks>
/// This is the failure that does not announce itself. <c>System.Text.Json</c> drops a key it does
/// not know and hands back a default for one that is missing, so a name that disagrees with the
/// bridge is not an exception — it is <c>null</c>, and the screen says "nothing is running" while
/// nothing fails. The Kotlin port has the same trap under <c>ignoreUnknownKeys</c> and the
/// TypeScript one under <c>undefined</c>; both repositories check their names against the source
/// for this reason.
/// </remarks>
public class WireTests
{
    private static string GoSource(string relative)
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !Directory.Exists(Path.Combine(dir.FullName, ".git")))
        {
            dir = dir.Parent;
        }
        Assert.True(dir is not null, "could not find the repository root above " + AppContext.BaseDirectory);
        var path = Path.Combine(dir!.FullName, relative.Replace('/', Path.DirectorySeparatorChar));
        Assert.True(File.Exists(path), $"the Go source moved: {relative}");
        return File.ReadAllText(path);
    }

    private static IEnumerable<string> JsonNames(Type t) =>
        t.GetProperties(BindingFlags.Public | BindingFlags.Instance)
         .Select(p => p.GetCustomAttribute<JsonPropertyNameAttribute>()?.Name)
         .Where(n => n is not null)!;

    /// <summary>
    /// Every name this client writes must appear in the bridge's own source.
    /// </summary>
    /// <remarks>
    /// A request field the bridge never reads is silently ignored, and the bridge then answers a
    /// different, valid question — no error anywhere, just a door that does nothing.
    /// </remarks>
    [Fact]
    public void EveryRequestFieldIsOneTheBridgeReads()
    {
        var go = GoSource("internal/adapter/idebridge/bridge.go");
        foreach (var name in JsonNames(typeof(BridgeRequest)))
        {
            Assert.True(go.Contains($"json:\"{name}\"") || go.Contains($"json:\"{name},"),
                $"the bridge's request struct has no field tagged \"{name}\"");
        }
    }

    /// <summary>
    /// Every name this client reads must be one the bridge writes.
    /// </summary>
    /// <remarks>
    /// The reply is assembled from string-keyed maps rather than a struct, so the check is for the
    /// key as written there. A name that agrees with nothing produces a permanent null.
    /// </remarks>
    [Fact]
    public void EveryReplyFieldIsOneTheBridgeWrites()
    {
        var go = GoSource("internal/adapter/idebridge/bridge.go") +
                 GoSource("internal/adapter/idebridge/activity.go");
        foreach (var name in JsonNames(typeof(BridgeReply)).Concat(JsonNames(typeof(DaemonInfo))))
        {
            Assert.True(go.Contains($"\"{name}\""), $"nothing in the bridge writes the key \"{name}\"");
        }
    }

    /// <summary>
    /// The activity vocabulary is one vocabulary. A word spelled differently on this side is a
    /// state that never matches — the screen would fall through to the default for ever.
    /// </summary>
    [Fact]
    public void TheActivityWordsAreSpeltTheWayTheBridgeSpellsThem()
    {
        var go = GoSource("internal/adapter/idebridge/activity.go");
        foreach (var word in new[]
                 {
                     ActivityState.NotRunning, ActivityState.Idle, ActivityState.Working,
                     ActivityState.Waiting, ActivityState.Unknown,
                 })
        {
            Assert.True(go.Contains($"\"{word}\""), $"the bridge does not know the word \"{word}\"");
        }
    }

    /// <summary>
    /// This client must not derive the socket path.
    /// </summary>
    /// <remarks>
    /// It is the first of the eight the bridge holds, and both existing ports got it wrong on the
    /// first try — with a non-standard FNV constant that must NOT be corrected, because the daemon
    /// uses it. Being wrong here is silent: this side looks for a socket nobody is on and reports a
    /// workspace that has a companion as not running. So the constant must not appear at all.
    /// </remarks>
    [Fact]
    public void TheSocketIsNotDerivedOnThisSide()
    {
        foreach (var file in Directory.EnumerateFiles(SourceDir(), "*.cs", SearchOption.AllDirectories))
        {
            var text = File.ReadAllText(file);
            Assert.DoesNotContain("1469598103934665603", text);
            Assert.DoesNotContain("1099511628211", text);
            Assert.False(text.Contains("daemon-") && text.Contains(".sock"),
                $"{Path.GetFileName(file)} builds a socket name; the bridge does that and reports it in about");
        }
    }

    /// <summary>
    /// Nothing writes a byte-order mark to the bridge.
    /// </summary>
    /// <remarks>
    /// Measured against the real bridge on Windows: three bytes of BOM and the first request comes
    /// back as <c>malformed request: invalid character 'U+FEFF' looking for beginning of value</c>.
    /// Only the first — so the symptom is a client whose handshake fails and whose next call works,
    /// which reads as a flaky bridge rather than a wrong encoding. <c>Encoding.UTF8</c> emits it by
    /// default, so the name itself is what the guard looks for.
    /// </remarks>
    [Fact]
    public void NothingUsesTheEncodingThatEmitsAByteOrderMark()
    {
        Assert.Empty(Wire.Utf8NoBom.GetPreamble());
        foreach (var file in Directory.EnumerateFiles(SourceDir(), "*.cs", SearchOption.AllDirectories))
        {
            // Code lines only. The rule is about what runs, and the reason it exists has to be
            // writable in a comment — a guard that forbade naming the trap would forbid explaining
            // it, and the next person would delete the fix for looking arbitrary.
            foreach (var line in File.ReadLines(file))
            {
                var code = line.TrimStart();
                if (code.StartsWith("//", StringComparison.Ordinal) || code.StartsWith('*')) continue;
                Assert.DoesNotContain("Encoding.UTF8", code);
            }
        }
    }

    internal static string SourceDir()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !Directory.Exists(Path.Combine(dir.FullName, ".git")))
        {
            dir = dir.Parent;
        }
        return Path.Combine(dir!.FullName, "clients", "visualstudio", "src", "Magi.Core");
    }
}
