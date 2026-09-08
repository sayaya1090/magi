using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace Magi.Core;

/// <summary>
/// The companion for one solution, spoken to through one <c>magi ide-bridge</c> child process.
/// </summary>
/// <remarks>
/// One bridge per workspace, because there is one companion per workspace.
/// <para>
/// The child's lifetime is tied to this object's, which is what stdio buys: no supervisor, no
/// second socket, and nothing left running when the window closes. The <b>companion</b> is the
/// opposite — it is started detached on purpose, so it outlives the window the way the terminal one
/// does.
/// </para>
/// </remarks>
public sealed class Companion : IDisposable
{
    private readonly Process _child;
    private readonly BridgeSession _session;
    private bool _disposed;

    public string Workspace { get; }

    private Companion(Process child, BridgeSession session, string workspace)
    {
        _child = child;
        _session = session;
        Workspace = workspace;
    }

    /// <summary>Start a bridge for this solution directory.</summary>
    /// <exception cref="FileNotFoundException">No binary anywhere. The message is <see cref="Locate.NoBinary"/>.</exception>
    public static Companion Open(string workspace, string? binary = null)
    {
        binary ??= Locate.Binary() ?? throw new FileNotFoundException(Locate.NoBinary);
        var start = new ProcessStartInfo(binary)
        {
            // The workspace is passed rather than relied on from the working directory: an IDE's
            // notion of "current directory" is not the solution's, and a bridge that guessed would
            // speak for a different companion without anything being wrong on the wire.
            ArgumentList = { "ide-bridge", "-workspace", workspace },
            RedirectStandardInput = true,
            RedirectStandardOutput = true,
            RedirectStandardError = true,
            UseShellExecute = false,
            CreateNoWindow = true,
            // No BOM on the way out — see Wire.Utf8NoBom. Reading one is harmless; writing one
            // makes the first request malformed and nothing after it.
            StandardOutputEncoding = Wire.Utf8NoBom,
            StandardInputEncoding = Wire.Utf8NoBom,
        };
        var child = Process.Start(start)
            ?? throw new IOException($"could not start {binary} ide-bridge");
        // Drained rather than ignored: a child whose stderr pipe fills stops writing, and a bridge
        // that stopped writing looks exactly like a companion with nothing to say.
        child.ErrorDataReceived += (_, _) => { };
        child.BeginErrorReadLine();
        return new Companion(child, new BridgeSession(child.StandardOutput, child.StandardInput), workspace);
    }

    /// <summary>What the bridge is, and what the companion advertises — or why it could not be asked.</summary>
    public Task<BridgeReply> AboutAsync(CancellationToken cancel = default) =>
        _session.AskAsync(new BridgeRequest { Method = "about" }, cancel: cancel);

    /// <summary>
    /// What it is doing, in the one word every client uses.
    /// </summary>
    /// <remarks>
    /// A failure to ask is an <see cref="ActivityState.Unknown"/> rather than an exception: this is
    /// polled, every caller would have to catch, and the vocabulary already has a word for "we
    /// could not ask". An exception here would make each screen invent that word again.
    /// </remarks>
    public async Task<Activity> ActivityAsync(string? session = null, CancellationToken cancel = default)
    {
        try
        {
            var reply = await _session.AskAsync(
                new BridgeRequest { Method = "activity", Session = session },
                TimeSpan.FromSeconds(10), cancel).ConfigureAwait(false);
            return Activity.Of(reply);
        }
        catch (Exception e) when (e is TimeoutException or IOException or ObjectDisposedException)
        {
            return Activity.Unknown with { Why = e.Message };
        }
    }

    /// <summary>
    /// Hand a request to the companion unchanged and get its answer unchanged.
    /// </summary>
    /// <remarks>
    /// Takes and returns raw JSON, which is the point: every door the daemon has — including the
    /// ones no editor client has ported — is reachable without a second vocabulary being invented
    /// for each one here.
    /// </remarks>
    public async Task<JsonElement?> DaemonAsync(JsonElement request, CancellationToken cancel = default)
    {
        var reply = await _session.AskAsync(
            new BridgeRequest { Method = "daemon", Req = request }, cancel: cancel).ConfigureAwait(false);
        if (!reply.Ok) throw new IOException(reply.Error ?? "the bridge refused the request");
        return reply.Resp;
    }

    /// <summary>
    /// Start a companion for this workspace, detached.
    /// </summary>
    /// <remarks>
    /// Detached and with its streams let go: a daemon tied to the IDE dies when the window closes,
    /// and the whole point is that it outlives the window the way the terminal one does.
    /// </remarks>
    public static void StartCompanion(string binary, string workspace)
    {
        var start = new ProcessStartInfo(binary)
        {
            ArgumentList = { "--daemon", "--detach" },
            WorkingDirectory = workspace,
            UseShellExecute = false,
            CreateNoWindow = true,
        };
        Process.Start(start)?.Dispose();
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;
        _session.Dispose();
        try
        {
            // Closing stdin is how the bridge is asked to stop — it reads until stdin closes. Kill
            // is the fallback for one that did not, not the first move.
            if (!_child.HasExited) _child.StandardInput.Close();
            if (!_child.WaitForExit(2000)) _child.Kill(entireProcessTree: true);
        }
        catch (InvalidOperationException) { /* already gone */ }
        catch (IOException) { /* the pipe went first */ }
        _child.Dispose();
    }
}
