using Microsoft.VisualStudio.Extensibility;
using Microsoft.VisualStudio.Extensibility.Commands;
using Microsoft.VisualStudio.Extensibility.Shell;

namespace Magi.Extension;

/// <summary>
/// The one way in: open the conversation.
/// </summary>
/// <remarks>
/// Declared in code rather than in a <c>.vsct</c> — the model has no such file, and the placement
/// is a property like everything else. Commands here run on a background thread, which is the right
/// shape for us: every call this client makes is a round trip to another process.
/// </remarks>
[VisualStudioContribution]
internal class OpenConversationCommand : Command
{
    /// <summary>
    /// The name comes from <c>string-resources.json</c>, keyed by the identifier between the
    /// percent signs.
    /// </summary>
    /// <remarks>
    /// A literal here builds, and warns: <c>CEE0027 — this string should be localized</c>. The
    /// analyzer only ever objects to that, the opposite mistake. <b>Nothing at build time checks
    /// the key itself</b> — measured, by naming one that exists in no resource file: it compiled,
    /// it packaged, and it warned about nothing. An unresolved <c>%key%</c> lands in
    /// <c>extension.json</c> as itself and shows up as that text in the menu, which is the first
    /// place anyone would see it. <c>ExtensionTests.EveryResourceKeyResolves</c> is the check.
    /// </remarks>
    public override CommandConfiguration CommandConfiguration => new("%Magi.OpenConversation.DisplayName%")
    {
        Placements = [CommandPlacement.KnownPlacements.ViewOtherWindowsMenu],
        Icon = new(ImageMoniker.KnownValues.Comment, IconSettings.IconAndText),
    };

    /// <inheritdoc/>
    public override async Task ExecuteCommandAsync(IClientContext context, CancellationToken cancellationToken)
    {
        await Extensibility.Shell().ShowToolWindowAsync<ConversationToolWindow>(activate: true, cancellationToken);
    }
}
