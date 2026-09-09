using Microsoft.VisualStudio.Extensibility;

namespace Magi.Extension;

/// <summary>
/// The extension itself: what Visual Studio loads, and the one place its parts are declared.
/// </summary>
/// <remarks>
/// Out-of-process, which is the decision the whole design turns on (DESIGN §1). This assembly runs
/// in its own process, so a mistake in it cannot stop somebody's IDE — and that matters more here
/// than in the sibling clients, because this client holds a socket open and keeps a child process
/// alive for the life of a window.
/// </remarks>
[VisualStudioContribution]
public class MagiExtension : Microsoft.VisualStudio.Extensibility.Extension
{
    /// <inheritdoc/>
    public override ExtensionConfiguration ExtensionConfiguration => new()
    {
        Metadata = new(
            id: "Magi.VisualStudio.a3f1c2d4-6b78-4e9a-8c15-2d7e0b9f4a63",
            version: this.ExtensionAssemblyVersion,
            publisherName: "sayaya1090",
            displayName: "magi",
            description: "Talk to the magi companion for this solution, and let it drive the IDE."),
    };
}
