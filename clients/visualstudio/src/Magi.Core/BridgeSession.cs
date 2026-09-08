using System.Collections.Concurrent;
using System.Text.Json;

namespace Magi.Core;

/// <summary>
/// One conversation with <c>magi ide-bridge</c>, over a pair of streams.
/// </summary>
/// <remarks>
/// Streams rather than a process on purpose: the framing, the correlation of replies to requests
/// and every failure mode below are testable without a binary, an IDE, or a companion — which
/// matters more here than in the sibling clients, because this editor has no first-party way to
/// drive a real extension in a test (DESIGN §6). <see cref="BridgeProcess"/> is the thin part that
/// supplies real streams.
/// <para>
/// ⚠ <b>Never close the write side to signal a request is finished.</b> The bridge reads until
/// stdin closes, and a half-close ends the session rather than the request — the same rule the
/// daemon's own socket contract states.
/// </para>
/// </remarks>
public sealed class BridgeSession : IDisposable
{
    private readonly TextWriter _out;
    private readonly TextReader _in;
    private readonly ConcurrentDictionary<int, TaskCompletionSource<BridgeReply>> _waiting = new();
    private readonly SemaphoreSlim _writing = new(1, 1);
    private readonly CancellationTokenSource _stopping = new();
    private readonly Task _reading;
    private int _nextId;
    private bool _disposed;

    /// <summary>Frames that carry a <c>sub</c> instead of an id, and arrive at any time.</summary>
    public event Action<BridgeReply>? Frame;

    public BridgeSession(TextReader input, TextWriter output)
    {
        _in = input;
        _out = output;
        _reading = Task.Run(ReadLoopAsync);
    }

    /// <summary>Ask, and wait for the reply carrying this request's id.</summary>
    /// <exception cref="TimeoutException">
    /// Thrown rather than returning a null that a caller could read as "it said nothing". A bridge
    /// that did not answer and a bridge that answered "nothing" are different facts.
    /// </exception>
    public async Task<BridgeReply> AskAsync(BridgeRequest request, TimeSpan? within = null,
                                            CancellationToken cancel = default)
    {
        ObjectDisposedException.ThrowIf(_disposed, this);
        request.Id = Interlocked.Increment(ref _nextId);
        var waiter = new TaskCompletionSource<BridgeReply>(TaskCreationOptions.RunContinuationsAsynchronously);
        _waiting[request.Id] = waiter;

        var line = JsonSerializer.Serialize(request, Wire.Json);
        try
        {
            // One writer at a time. Two interleaving would put half of one JSON object inside
            // another — a line no reader can parse and no error names.
            await _writing.WaitAsync(cancel).ConfigureAwait(false);
            try
            {
                await _out.WriteAsync(line.AsMemory(), cancel).ConfigureAwait(false);
                await _out.WriteAsync("\n".AsMemory(), cancel).ConfigureAwait(false);
                await _out.FlushAsync(cancel).ConfigureAwait(false);
            }
            finally { _writing.Release(); }
        }
        catch (Exception e)
        {
            _waiting.TryRemove(request.Id, out _);
            throw new IOException($"could not send {request.Method} to the bridge", e);
        }

        using var deadline = CancellationTokenSource.CreateLinkedTokenSource(cancel, _stopping.Token);
        var timeout = within ?? TimeSpan.FromSeconds(30);
        var finished = await Task.WhenAny(waiter.Task, Task.Delay(timeout, deadline.Token)).ConfigureAwait(false);
        // An answer that arrived beats the clock, even when the clock stopped first. Disposing
        // cancels the delay AND settles every waiter, and whichever of those two the runtime
        // notices first must not decide whether the caller sees the answer it already has.
        if (waiter.Task.IsCompleted)
        {
            deadline.Cancel();
            return await waiter.Task.ConfigureAwait(false);
        }
        if (finished != waiter.Task)
        {
            // Take the waiter out rather than leaving it to catch somebody else's reply. Replies
            // are matched by id, so a stale waiter is only a leak — but a leak that never completes
            // is a handle held for the life of the window.
            _waiting.TryRemove(request.Id, out _);
            cancel.ThrowIfCancellationRequested();
            throw new TimeoutException($"the bridge did not answer {request.Method} within {timeout.TotalSeconds:0}s");
        }
        deadline.Cancel();
        return await waiter.Task.ConfigureAwait(false);
    }

    private async Task ReadLoopAsync()
    {
        try
        {
            while (await _in.ReadLineAsync(_stopping.Token).ConfigureAwait(false) is { } line)
            {
                if (string.IsNullOrWhiteSpace(line)) continue;
                BridgeReply? reply;
                try
                {
                    reply = JsonSerializer.Deserialize<BridgeReply>(line, Wire.Json);
                }
                catch (JsonException)
                {
                    // A line that is not JSON is not a reply to anything. Dropping it keeps every
                    // waiter matched by id; treating it as an answer would hand a caller garbage.
                    continue;
                }
                if (reply is null) continue;
                // A line whose id matches nobody is not an answer. Today that is the malformed-line
                // reply the bridge sends with no id to answer to; when subscriptions land it is
                // also their frames. Either way it must not be handed to whoever asked last —
                // answering a question with somebody else's news is worse than not answering.
                if (_waiting.TryRemove(reply.Id, out var waiter)) waiter.TrySetResult(reply);
                else Frame?.Invoke(reply);
            }
        }
        catch (OperationCanceledException) { /* disposing */ }
        catch (IOException) { /* the bridge went away; Finish says so below */ }
        finally { Finish(); }
    }

    /// <summary>
    /// Everybody still waiting gets an answer, because a task nobody completes is a screen that
    /// says nothing for ever.
    /// </summary>
    private void Finish()
    {
        foreach (var id in _waiting.Keys)
        {
            if (_waiting.TryRemove(id, out var waiter))
            {
                waiter.TrySetResult(new BridgeReply { Id = id, Ok = false, Error = "the bridge closed" });
            }
        }
    }

    public void Dispose()
    {
        if (_disposed) return;
        _disposed = true;
        // Settle first, then stop the clocks. The other order leaves a caller being told its
        // request timed out at the moment the answer "the bridge closed" was ready for it.
        Finish();
        _stopping.Cancel();
        try { _reading.Wait(TimeSpan.FromSeconds(2)); } catch (AggregateException) { /* already done */ }
        _stopping.Dispose();
        _writing.Dispose();
    }
}
