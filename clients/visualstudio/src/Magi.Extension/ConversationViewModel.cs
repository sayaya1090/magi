using Magi.Core;
using Microsoft.VisualStudio.Extensibility.UI;

namespace Magi.Extension;

/// <summary>
/// What the conversation panel shows, as data.
/// </summary>
/// <remarks>
/// Remote UI is XAML that Visual Studio instantiates in <b>its</b> process, and the documentation is
/// explicit that it "doesn't allow referencing your own custom controls" and supports no code-behind
/// or event handlers. So there is nowhere to put logic beside the view: everything the panel shows
/// has to arrive here as a property, and everything it does has to be a command. That is not a
/// style preference — it is what the platform leaves.
/// </remarks>
public class ConversationViewModel : NotifyPropertyChangedObject
{
    private string _state = ActivityState.Unknown;
    private string _label = "cannot say";
    private string _detail = "";
    private string _workspace = "";
    private string _draft = "";
    private string _lastAnswer = "";
    private bool _busy;

    /// <summary>The raw word, for anything that wants to branch on it.</summary>
    public string State
    {
        get => _state;
        set => SetProperty(ref _state, value);
    }

    /// <summary>The sentence a person reads. One vocabulary — see <see cref="Activity.Label"/>.</summary>
    public string Label
    {
        get => _label;
        set => SetProperty(ref _label, value);
    }

    /// <summary>
    /// The line under it: what it is running on, or why there was nothing to ask.
    /// </summary>
    /// <remarks>
    /// Kept separate from <see cref="Label"/> because the two change on different clocks — one every
    /// poll, the other when somebody changes a setting — and folding them makes every tick redraw a
    /// footer that did not move.
    /// </remarks>
    public string Detail
    {
        get => _detail;
        set => SetProperty(ref _detail, value);
    }

    /// <summary>What the person is typing.</summary>
    public string Draft
    {
        get => _draft;
        set => SetProperty(ref _draft, value);
    }

    /// <summary>
    /// The companion's answer to the last thing sent — or the reason it did not take.
    /// </summary>
    /// <remarks>
    /// ⚠ This is not the conversation. A transcript is a stream, and the bridge's forwarding door
    /// answers one request with one reply, so no stream crosses it yet. It arrives with
    /// <c>watch</c> (<c>docs/IDE_BRIDGE</c> §5). Showing a receipt and saying so beats drawing an
    /// empty panel that looks broken.
    /// </remarks>
    public string LastAnswer
    {
        get => _lastAnswer;
        set => SetProperty(ref _lastAnswer, value);
    }

    /// <summary>True while a send is in flight, so the panel can stop a second one.</summary>
    public bool Busy
    {
        get => _busy;
        set
        {
            if (SetProperty(ref _busy, value)) RaiseNotifyPropertyChangedEvent(nameof(CanSend));
        }
    }

    /// <summary>
    /// Bound straight to <c>IsEnabled</c> and <c>Visibility</c>, with no converter in between.
    /// </summary>
    /// <remarks>
    /// A converter would have to be a type from this assembly, and the XAML runs in Visual Studio's
    /// process where "Remote UI doesn't allow referencing your own custom controls". The failure
    /// would be at parse time in the IDE, which this project's build never sees — so the shape the
    /// view needs is computed here instead. The visibility is a word because WPF converts a string
    /// to the enum on the way in.
    /// </remarks>
    public bool CanSend => !_busy;

    /// <summary>Drawn only when nobody is listening.</summary>
    public string StartVisibility => State == ActivityState.NotRunning ? "Visible" : "Collapsed";

    /// <summary>
    /// The tree this panel speaks for, shown at the head of <see cref="Detail"/>.
    /// </summary>
    /// <remarks>
    /// Set once, before the first reading. It is on the screen rather than only in a field because
    /// the failure it guards against is silent: a companion answering perfectly well for a
    /// different directory looks exactly like the right one.
    /// </remarks>
    public string Workspace
    {
        get => _workspace;
        set => SetProperty(ref _workspace, value);
    }

    public IAsyncCommand? Send { get; set; }
    public IAsyncCommand? StartCompanion { get; set; }

    /// <summary>Take one reading, and move only what moved.</summary>
    public void Show(Activity activity)
    {
        State = activity.State;
        Label = activity.Label();
        var setup = activity.Setup;
        var parts = new List<string>();
        foreach (var key in new[] { "model", "backend", "permission" })
        {
            if (setup.TryGetValue(key, out var value)) parts.Add($"{key}: {value}");
        }
        // The reason comes last and only when there is nothing else — a companion that is running
        // has settings worth reading, and one that is not has a reason worth reading.
        if (parts.Count == 0 && !string.IsNullOrWhiteSpace(activity.Why)) parts.Add(activity.Why!);
        // The tree goes first, ahead of both. Which companion is being spoken for is the one thing
        // on this line that cannot be inferred from anything else on the screen.
        if (!string.IsNullOrEmpty(Workspace)) parts.Insert(0, Workspace);
        Detail = string.Join("  ·  ", parts);
        RaiseNotifyPropertyChangedEvent(nameof(StartVisibility));
    }
}
