using System.Text.Json;
using Magi.Core;

namespace Magi.Extension;

/// <summary>
/// The one companion this window talks to, and the one bridge that reaches it.
/// </summary>
/// <remarks>
/// One per solution, because there is one companion per workspace. The bridge child process is
/// opened lazily — an IDE that opens a solution the person never asks magi about should not have
/// started anything — and dropped when it dies, so the next question opens a fresh one rather than
/// talking into a closed pipe.
/// <para>
/// Nothing here derives a socket path or decides what a state means. Those are the bridge's, and
/// this type exists mostly to own a lifetime.
/// </para>
/// </remarks>
internal sealed class SolutionCompanion : IDisposable
{
    private readonly SemaphoreSlim _gate = new(1, 1);
    private readonly string _workspace;
    private Companion? _open;
    private bool _disposed;

    public SolutionCompanion(string workspace) => _workspace = workspace;

    /// <summary>What it is doing, or the reason we could not find out.</summary>
    public async Task<Activity> ActivityAsync(CancellationToken cancel = default)
    {
        var companion = await ReachAsync(cancel).ConfigureAwait(false);
        if (companion is null) return Activity.Unknown with { Why = Locate.NoBinary };
        var reading = await companion.ActivityAsync(cancel: cancel).ConfigureAwait(false);
        // A bridge that has gone away answers unknown for ever otherwise: the child is dead and
        // every later question goes into the same closed pipe. Drop it and let the next one dial.
        //
        // Only when it did not answer, though. `unknown` is also a word the bridge SAYS, over a
        // pipe that is working, about a daemon it could not ask — a socket path past the address
        // limit, or a status round trip that failed. Dropping on that kills a healthy child and
        // starts another, and the path-length one never improves: the panel would spawn a process
        // every ten seconds for the life of the window and show the same sentence each time.
        if (reading.State == ActivityState.Unknown && !reading.Answered) Drop();
        return reading;
    }

    /// <summary>
    /// Send a turn, and hand back whatever the companion said.
    /// </summary>
    /// <remarks>
    /// Raw JSON both ways, through the bridge's forwarding door. Inventing a typed method per door
    /// here is how a client ends up with a second vocabulary for each one — the thing the door
    /// exists to avoid.
    /// </remarks>
    public async Task<string> SubmitAsync(string text, CancellationToken cancel = default)
    {
        var companion = await ReachAsync(cancel).ConfigureAwait(false);
        if (companion is null) return Locate.NoBinary;
        var request = JsonSerializer.SerializeToElement(new { method = "submit", text });
        try
        {
            var answer = await companion.DaemonAsync(request, cancel).ConfigureAwait(false);
            return answer?.ToString() ?? "(the companion answered nothing)";
        }
        catch (Exception e) when (e is IOException or TimeoutException)
        {
            Drop();
            return e.Message;
        }
    }

    /// <summary>Start a companion for this solution, detached, so it outlives the window.</summary>
    public string? Start()
    {
        var binary = Locate.Binary();
        if (binary is null) return Locate.NoBinary;
        Companion.StartCompanion(binary, _workspace);
        return null;
    }

    private async Task<Companion?> ReachAsync(CancellationToken cancel)
    {
        if (_open is not null) return _open;
        // Read before the gate, not only behind it. Dispose sets this and then disposes the gate,
        // so a caller that arrives afterwards would otherwise wait on a disposed semaphore and get
        // an ObjectDisposedException — thrown, on the way out of a window that is already closing.
        // This narrows that window rather than closing it: whoever is already inside still has to
        // be caught, and PollAsync is where that happens.
        if (_disposed) return null;
        await _gate.WaitAsync(cancel).ConfigureAwait(false);
        try
        {
            if (_open is not null || _disposed) return _open;
            try { _open = Companion.Open(_workspace); }
            catch (Exception e) when (e is FileNotFoundException or IOException) { return null; }
            return _open;
        }
        finally { _gate.Release(); }
    }

    private void Drop()
    {
        var going = Interlocked.Exchange(ref _open, null);
        going?.Dispose();
    }

    public void Dispose()
    {
        _disposed = true;
        Drop();
        _gate.Dispose();
    }
}
