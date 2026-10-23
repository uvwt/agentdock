using System;
using System.Collections.Generic;
using System.Globalization;
using System.Linq;

namespace AgentDock.ControlPanel
{
    internal static class AcpProfileId
    {
        internal static string Create(string displayName, IEnumerable<string> existingIds)
        {
            var idBase = new string(displayName.ToLowerInvariant().Select(character =>
                character >= 'a' && character <= 'z' || character >= '0' && character <= '9' ||
                character == '.' || character == '_' || character == '-' ? character : '-').ToArray()).Trim('-');
            if (idBase.Length == 0) idBase = "custom";
            if (idBase.Length > 64) idBase = idBase.Substring(0, 64);
            var existing = new HashSet<string>(existingIds, StringComparer.OrdinalIgnoreCase);
            var id = idBase;
            for (var suffix = 2; existing.Contains(id); suffix++)
            {
                var ending = "-" + suffix.ToString(CultureInfo.InvariantCulture);
                id = idBase.Substring(0, Math.Min(idBase.Length, 64 - ending.Length)) + ending;
            }
            return id;
        }
    }
}
