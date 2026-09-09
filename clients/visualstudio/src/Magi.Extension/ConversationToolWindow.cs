using Magi.Core;
using Microsoft.VisualStudio.Extensibility;
using Microsoft.VisualStudio.Extensibility.ToolWindows;
using Microsoft.VisualStudio.Extensibility.UI;
using Microsoft.VisualStudio.ProjectSystem.Query;
using Microsoft.VisualStudio.RpcContracts.RemoteUI;

namespace Magi.Extension;

/// <summary>
/// The conversation, as a dockable tool window.
/// </summary>
/// <remarks>
/// A tool window rather than a dialog because the companion is the solution's, not a moment's: it
/// keeps running while the person works, and a modal would be wrong about what it is. There are two
/// panels planned and no more — the conversation and, later, the plan — which is our rule rather
/// than the platform's.
/// <para>
/// ⚠ The transcript is not here yet. A transcript is a stream and the bridge's forwarding door
/// answers one request with one reply, so nothing streams across it until <c>watch</c> exists
/// (<c>docs/IDE_BRIDGE</c> §5). The panel says what it can say and does not pretend the rest is
/// empty.
/// </para>
/// </remarks>
[VisualStudioContribution]
internal class ConversationToolWindow : ToolWindow
{
    private readonly ConversationViewModel _model = new();
    private SolutionCompanion? _companion;
    private CancellationTokenSource? _polling;

    public ConversationToolWindow()
    {
        this.Title = "magi";
    }

    /// <inheritdoc/>
    public override ToolWindowConfiguration ToolWindowConfiguration => new()
    {
        // Docked right, not in the document well. The well is for documents, and a conversation is
        // not one — it is a thing a person watches while they edit, which is what the right dock is
        // for in this IDE. This is the one placement decision each editor re-makes for itself
        // (PLATFORM §4); VS Code put the conversation in the panel because that is that editor's
        // convention, and copying it here would put a permanent panel across the bottom of an IDE
        // whose users keep that space for output.
        //
        // It takes two properties, not one: Placement says *what* to dock against and the only
        // things it can name are the document well, a floating window, or another tool window by
        // Guid — there is no Placement meaning "the right dock". DockDirection then says which side
        // of that target. So the right dock is spelled well + right, and the pair below is the
        // whole of it.
        Placement = ToolWindowPlacement.DocumentWell,
        DockDirection = Dock.Right,

        // Not opened for somebody who never asked. The command is the way in.
        AllowAutoCreation = false,
    };

    /// <inheritdoc/>
    public override Task<IRemoteUserControl> GetContentAsync(CancellationToken cancellationToken)
    {
        _model.Send = new AsyncCommand(async (_, _, cancel) => await SendAsync(cancel));
        _model.StartCompanion = new AsyncCommand(async (_, _, cancel) => await StartAsync(cancel));
        return Task.FromResult<IRemoteUserControl>(new ConversationControl(_model));
    }

    /// <inheritdoc/>
    public override async Task InitializeAsync(CancellationToken cancellationToken)
    {
        await base.InitializeAsync(cancellationToken);
        var found = await WorkspaceAsync(cancellationToken).ConfigureAwait(false);
        _companion = new SolutionCompanion(found.Directory);
        // Onto the panel, not only into the field. A bridge pointed at the wrong tree speaks for a
        // different companion with nothing wrong anywhere on the wire — the one place that can go
        // visibly wrong is a screen that says which tree it means.
        _model.Workspace = found.Guessed
            ? found.Directory + "  (no solution open — the IDE's own directory)"
            : found.Directory;
        _polling = new CancellationTokenSource();
        _ = PollAsync(_polling.Token);
    }

    /// <summary>
    /// Which tree this panel speaks for, and whether we actually know.
    /// </summary>
    /// <remarks>
    /// <c>ISolutionSnapshot.Directory</c>, asked through the Project Query API — the door that
    /// answers this, and the reason the question lives up here at all: <c>Magi.Core</c> must not
    /// know what a solution is.
    /// <para>
    /// The fall-back is the IDE's current directory, which is what this used to be in every case.
    /// It is a guess, and it is labelled as one on the panel rather than quietly substituted —
    /// with no solution open there is no better answer, and somebody who can see the path can tell
    /// at a glance that magi is speaking for somewhere else.
    /// </para>
    /// <para>
    /// ⚠ Asked once, at open. Opening a different solution in the same window leaves this pointing
    /// at the old one; the panel goes on showing the old path, which is at least the visible kind
    /// of wrong. Re-asking every poll would put a cross-process query on a two-second clock.
    /// </para>
    /// </remarks>
    private async Task<(string Directory, bool Guessed)> WorkspaceAsync(CancellationToken cancel)
    {
        try
        {
            var solutions = await Extensibility.Workspaces()
                .QuerySolutionAsync(query => query.With(solution => solution.Directory), cancel)
                .ConfigureAwait(false);
            var directory = solutions
                .Select(solution => solution.Directory)
                .FirstOrDefault(d => !string.IsNullOrWhiteSpace(d));
            if (directory is not null) return (directory, false);
        }
        catch (Exception e) when (e is QueryExecutionException or InvalidOperationException)
        {
            // No solution open is not an error worth a dialog, and neither is a query the shell
            // declined to answer. Both land on the same honest fall-back.
        }
        return (Environment.CurrentDirectory, true);
    }

    /// <summary>
    /// Ask every couple of seconds, and slow right down when there is nothing there.
    /// </summary>
    /// <remarks>
    /// Polling a companion that does not exist costs a wake-up per tick and tells nobody anything —
    /// a socket appearing is not something a person is waiting on the panel to notice.
    /// <para>
    /// Nothing awaits this task — it is started with <c>_ =</c> — so an exception leaving it is not
    /// reported anywhere at all: it becomes an unobserved task exception inside somebody's IDE
    /// process, and what they see is a panel frozen on its last reading. A stopped panel showing
    /// "attached" is indistinguishable from a companion that is merely running. Hence the two
    /// catches, each with one job.
    /// </para>
    /// </remarks>
    private async Task PollAsync(CancellationToken cancel)
    {
        try
        {
            while (!cancel.IsCancellationRequested)
            {
                Activity reading;
                try
                {
                    reading = _companion is null
                        ? Activity.Unknown
                        : await _companion.ActivityAsync(cancel).ConfigureAwait(false);
                }
                catch (Exception e) when (e is not OperationCanceledException
                                            and not ObjectDisposedException)
                {
                    // One question that failed is not the end of the asking. Magi.Core already
                    // folds the expected failures into `unknown`, so anything reaching here is a
                    // surprise — and a surprise that stops the panel for ever is worse than one
                    // that puts its own message on the screen.
                    reading = Activity.Unknown with { Why = e.Message };
                }
                _model.Show(reading);
                var wait = reading.State switch
                {
                    ActivityState.NotRunning => TimeSpan.FromSeconds(10),
                    ActivityState.Unknown => TimeSpan.FromSeconds(10),
                    _ => TimeSpan.FromSeconds(2),
                };
                await Task.Delay(wait, cancel).ConfigureAwait(false);
            }
        }
        catch (Exception e) when (e is OperationCanceledException or ObjectDisposedException)
        {
            // The window went while we were asking, or waiting. Both are the normal way this loop
            // ends and neither is worth reporting — but they have to be caught here, because there
            // is nowhere else they could be.
        }
    }

    private async Task SendAsync(CancellationToken cancel)
    {
        var text = _model.Draft.Trim();
        if (text.Length == 0 || _companion is null) return;
        _model.Busy = true;
        try
        {
            _model.Draft = "";
            _model.LastAnswer = await _companion.SubmitAsync(text, cancel).ConfigureAwait(false);
        }
        finally { _model.Busy = false; }
    }

    private async Task StartAsync(CancellationToken cancel)
    {
        if (_companion is null) return;
        var complaint = _companion.Start();
        if (complaint is not null) { _model.LastAnswer = complaint; return; }
        // The daemon takes a moment to open its socket. Asking immediately would report the
        // absence it was just told to fix.
        try { await Task.Delay(TimeSpan.FromSeconds(1), cancel).ConfigureAwait(false); }
        catch (OperationCanceledException) { return; }
        _model.Show(await _companion.ActivityAsync(cancel).ConfigureAwait(false));
    }

    /// <inheritdoc/>
    protected override void Dispose(bool disposing)
    {
        if (disposing)
        {
            _polling?.Cancel();
            _polling?.Dispose();
            _companion?.Dispose();
        }
        base.Dispose(disposing);
    }
}
