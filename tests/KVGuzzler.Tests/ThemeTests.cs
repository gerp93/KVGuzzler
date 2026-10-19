using Avalonia.Controls;
using Avalonia.Headless.XUnit;
using KVGuzzler.Data;
using KVGuzzler.Services;
using KVGuzzler.Views;
using VisualAssault;
using Xunit;

namespace KVGuzzler.Tests;

public class ThemeTests
{
    [AvaloniaFact]
    public void StartsOnTheDefaultTheme()
    {
        Assert.Equal(ThemeChoice.DefaultId, ThemeManager.Current?.Id);
        Assert.NotNull(VisualAssaultThemes.Find(ThemeChoice.DefaultId));
    }

    [AvaloniaFact]
    public void SavedThemeIsAppliedAtStartup()
    {
        using var t = new TempData();
        ThemeChoice.Save(t.Services.Database, "retrowave");
        ThemeChoice.ApplySaved(t.Services.Database);
        Assert.Equal("retrowave", ThemeManager.Current?.Id);
        ThemeManager.Apply(ThemeChoice.DefaultId);
    }

    [AvaloniaFact]
    public void UnknownSavedThemeFallsBackToTheDefault()
    {
        using var t = new TempData();
        t.Services.Database!.SetSetting(SettingKeys.Theme, "no_such_theme");
        ThemeChoice.ApplySaved(t.Services.Database);
        Assert.Equal(ThemeChoice.DefaultId, ThemeManager.Current?.Id);
    }

    [AvaloniaFact]
    public void PickingAThemeInSettingsAppliesAndSavesIt()
    {
        using var t = new TempData();
        var window = new MainWindow(t.Services);
        window.Show();
        var box = window.FindControl<ComboBox>("ThemeBox")!;
        Assert.Equal(VisualAssaultThemes.All.Count, box.ItemCount);

        var index = VisualAssaultThemes.All.ToList().FindIndex(x => x.Id == "bubblegum");
        box.SelectedIndex = index;

        Assert.Equal("bubblegum", ThemeManager.Current?.Id);
        Assert.Equal("bubblegum", t.Services.Database!.GetSetting(SettingKeys.Theme));
        ThemeManager.Apply(ThemeChoice.DefaultId);
    }
}
