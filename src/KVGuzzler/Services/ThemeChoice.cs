using KVGuzzler.Data;
using VisualAssault;

namespace KVGuzzler.Services;

/// <summary>Which VisualAssault theme the user picked. Stored as a setting; an unknown id (a theme
/// removed upstream, a hand-edited database) falls back to the default instead of failing.</summary>
public static class ThemeChoice
{
    public const string DefaultId = "blue_oval";

    public static void ApplySaved(Database? database)
    {
        var saved = database?.GetSetting(SettingKeys.Theme);
        ThemeManager.Apply(saved is not null && VisualAssaultThemes.Find(saved) is { } theme ? theme.Id : DefaultId);
    }

    public static void Save(Database? database, string id) => database?.SetSetting(SettingKeys.Theme, id);
}
