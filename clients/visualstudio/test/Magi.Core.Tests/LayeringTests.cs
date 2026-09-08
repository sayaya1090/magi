using System.Reflection;
using Xunit;

namespace Magi.Core.Tests;

/// <summary>
/// <c>Magi.Core</c> must not reach the IDE.
/// </summary>
/// <remarks>
/// The same guard as the JetBrains plugin's <c>ArchitectureTest</c> and the VS Code port's
/// <c>layering.test.ts</c>, and it earns its place here more than in either: this editor has no
/// first-party way to drive a real extension in a test (DESIGN §6), so whatever leaks out of this
/// project stops being measured at all. Without the guard it leaks a line at a time, each one
/// convenient.
/// </remarks>
public class LayeringTests
{
    [Fact]
    public void TheCoreDoesNotReferenceTheExtensibilitySdk()
    {
        var core = typeof(Companion).Assembly;
        foreach (var referenced in core.GetReferencedAssemblies())
        {
            var name = referenced.Name ?? "";
            Assert.False(name.StartsWith("Microsoft.VisualStudio", StringComparison.Ordinal),
                $"Magi.Core references {name}; the IDE belongs in Magi.Extension, and what is in " +
                "here is what gets measured");
        }
    }

    /// <summary>
    /// The source must not name it either. A reference can be gone from the compiled assembly while
    /// a using directive sits in the file waiting for somebody to add the package back.
    /// </summary>
    [Fact]
    public void NoSourceFileEvenMentionsIt()
    {
        foreach (var file in Directory.EnumerateFiles(WireTests.SourceDir(), "*.cs", SearchOption.AllDirectories))
        {
            var text = File.ReadAllText(file);
            Assert.DoesNotContain("using Microsoft.VisualStudio", text);
        }
    }

    /// <summary>
    /// The project file must not reference it either — that is where it would come back.
    /// </summary>
    [Fact]
    public void TheProjectFileDoesNotPullItIn()
    {
        var csproj = Path.Combine(WireTests.SourceDir(), "Magi.Core.csproj");
        Assert.DoesNotContain("Microsoft.VisualStudio.Extensibility", File.ReadAllText(csproj));
    }

    /// <summary>
    /// Everything public here has to be readable by somebody who has never opened Visual Studio,
    /// which is the point of the split. A type that only makes sense inside the IDE is a sign the
    /// line moved.
    /// </summary>
    [Fact]
    public void ThePublicSurfaceIsSmallEnoughToRead()
    {
        var types = typeof(Companion).Assembly.GetExportedTypes()
            .Where(t => !t.IsNested)
            .Select(t => t.Name)
            .OrderBy(n => n, StringComparer.Ordinal)
            .ToArray();
        Assert.Equal(
            new[] { "Activity", "ActivityState", "BridgeReply", "BridgeRequest", "BridgeSession",
                    "Companion", "DaemonInfo", "Locate", "Wire" },
            types);
    }
}
