using Avalonia.Controls;
using Avalonia.Interactivity;

namespace KVGuzzler.Views;

public partial class MissingDatabaseWindow : Window
{
    // Needed by Avalonia's XAML runtime loader and previewer; never called at runtime.
    public MissingDatabaseWindow() => throw new NotSupportedException("Create the window with its arguments.");

    public MissingDatabaseWindow(string missingPath, Action useDefault, Action quit)
    {
        InitializeComponent();
        MessageText.Text = $"You chose to keep your database at:\n{missingPath}\nbut nothing is there now.";
        UseDefaultButton.Click += (_, _) => useDefault();
        QuitButton.Click += (_, _) => quit();
    }
}
