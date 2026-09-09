using Magi.Core;
using Microsoft.VisualStudio.Extensibility;
using Microsoft.VisualStudio.Extensibility.ToolWindows;
using Microsoft.VisualStudio.Extensibility.UI;
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
        _companion = new SolutionCompanion(WorkspaceOf());
        _polling = new CancellationTokenSource();
        _ = PollAsync(_polling.Token);
    }

    /// <summary>
    /// Which tree this panel speaks for.
    /// </summary>
    /// <remarks>
    /// ⚠ The current directory, which is the IDE's and not necessarily the solution's. Reading the
    /// open solution needs the Project Query API and belongs with the rest of the spine; until then
    /// this is stated rather than hidden, because a bridge pointed at the wrong tree speaks for a
    /// different companion with nothing wrong on the wire.
    /// </remarks>
    private static string WorkspaceOf() => Environment.CurrentDirectory;

    /// <summary>
    /// Ask every couple of seconds, and slow right down when there is nothing there.
    /// </summary>
    /// <remarks>
    /// Polling a companion that does not exist costs a wake-up per tick and tells nobody anything —
    /// a socket appearing is not something a person is waiting on the panel to notice.
    /// </remarks>
    private async Task PollAsync(CancellationToken cancel)
    {
        while (!cancel.IsCancellationRequested)
        {
            var reading = _companion is null
                ? Activity.Unknown
                : await _companion.ActivityAsync(cancel).ConfigureAwait(false);
            _model.Show(reading);
            var wait = reading.State switch
            {
                ActivityState.NotRunning => TimeSpan.FromSeconds(10),
                ActivityState.Unknown => TimeSpan.FromSeconds(10),
                _ => TimeSpan.FromSeconds(2),
            };
            try { await Task.Delay(wait, cancel).ConfigureAwait(false); }
            catch (OperationCanceledException) { return; }
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
