using Microsoft.VisualStudio.Extensibility.UI;

namespace Magi.Extension;

/// <summary>
/// The panel's XAML, paired with the data it draws.
/// </summary>
/// <remarks>
/// The XAML is looked up by this type's name from the embedded resources — which is why the file is
/// an <c>EmbeddedResource</c> and removed from the <c>Page</c> pipeline in the project file. It is
/// never compiled into this assembly: Visual Studio parses it in its own process.
/// </remarks>
internal class ConversationControl(ConversationViewModel model) : RemoteUserControl(model)
{
}
