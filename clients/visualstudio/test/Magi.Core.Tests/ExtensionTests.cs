using System.Text.RegularExpressions;
using Xunit;

namespace Magi.Core.Tests;

/// <summary>
/// The edges of <c>Magi.Extension</c> that nothing else measures.
/// </summary>
/// <remarks>
/// This project cannot reference that assembly — <c>LayeringTests</c> is the reason, and DESIGN §6
/// is why there is no harness for the real extension. So these read the extension as text, the same
/// way <c>WireTests</c> reads the bridge's Go source, and they guard the three things that were
/// measured to build clean and fail only once Visual Studio has it:
/// <list type="bullet">
/// <item>a <c>%key%</c> that resolves to nothing — a key that exists nowhere builds with zero
/// warnings, and the menu then draws the percent signs</item>
/// <item>the resource file left undeclared — it is not packaged, so every key is unresolved and the
/// menu draws the percent signs again, from the other direction</item>
/// <item>a binding with no property behind it — the XAML is parsed in Visual Studio's process, and
/// this project's build never sees it</item>
/// </list>
/// </remarks>
public class ExtensionTests
{
    private static string ExtensionDir()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !Directory.Exists(Path.Combine(dir.FullName, ".git")))
        {
            dir = dir.Parent;
        }
        return Path.Combine(dir!.FullName, "clients", "visualstudio", "src", "Magi.Extension");
    }

    private static string Read(string name) => File.ReadAllText(Path.Combine(ExtensionDir(), name));

    /// <summary>
    /// Every <c>%key%</c> the extension names has a string behind it.
    /// </summary>
    /// <remarks>
    /// Measured on 2026-09-09: a key that appears in no resource file compiles, packages, and warns
    /// about nothing. The analyzer only objects to a bare literal (CEE0027), which is the opposite
    /// mistake — so nothing at build time tells a resolved key from an unresolved one, and this is
    /// the check instead.
    /// </remarks>
    [Fact]
    public void EveryResourceKeyResolves()
    {
        var resources = Read("string-resources.json");
        foreach (var file in Directory.EnumerateFiles(ExtensionDir(), "*.cs", SearchOption.AllDirectories))
        {
            foreach (var line in File.ReadLines(file))
            {
                // Code lines only. The prose above these declarations has to be able to say
                // "%key%" while explaining the trap, the same exception WireTests makes.
                var code = line.TrimStart();
                if (code.StartsWith("//", StringComparison.Ordinal) || code.StartsWith('*')) continue;
                foreach (Match m in Regex.Matches(code, @"%([A-Za-z][A-Za-z0-9_.]*)%"))
                {
                    var key = m.Groups[1].Value;
                    Assert.True(resources.Contains($"\"{key}\""),
                        $"{Path.GetFileName(file)} names %{key}%, which string-resources.json does " +
                        "not define; the menu would show the percent signs");
                }
            }
        }
    }

    /// <summary>
    /// The resource file is declared, and so ships.
    /// </summary>
    /// <remarks>
    /// It was not, and the vsix went out without it. A <c>.json</c> beside the sources is a
    /// <c>None</c> item to the SDK — nothing collects it by convention, and the build says nothing
    /// about a manifest whose every name is a key it cannot resolve.
    /// </remarks>
    [Fact]
    public void TheResourceFileIsPackaged()
    {
        Assert.Contains("string-resources.json", Read("Magi.Extension.csproj"));
    }

    /// <summary>
    /// Every binding in the panel has a property to bind to.
    /// </summary>
    /// <remarks>
    /// Remote UI hands the XAML to Visual Studio as text and it is parsed over there, so a renamed
    /// property is not a compile error here — it is a blank in a panel, in somebody else's process.
    /// </remarks>
    [Fact]
    public void EveryBindingHasAPropertyBehindIt()
    {
        var model = Read("ConversationViewModel.cs");
        var xaml = Read("ConversationControl.xaml");
        var bound = Regex.Matches(xaml, @"\{Binding\s+([A-Za-z_][A-Za-z0-9_]*)")
            .Select(m => m.Groups[1].Value)
            .Distinct()
            .ToArray();
        Assert.NotEmpty(bound);
        foreach (var name in bound)
        {
            Assert.True(Regex.IsMatch(model, $@"public\s+[\w\?<>,\.\[\]]+\s+{name}\b"),
                $"the panel binds {name}, which ConversationViewModel does not expose");
        }
    }
}
