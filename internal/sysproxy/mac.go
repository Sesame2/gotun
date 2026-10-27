package sysproxy

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
)

type macSettings map[string]map[string]any

// SystemConfiguration preserves absent fields and authentication flags that
// networksetup's text output cannot round-trip without changing the old proxy.
const macPreferences = `ObjC.import('SystemConfiguration');
var prefs = $.SCPreferencesCreate(null, $('GoTun'), null);
if (!prefs) throw Error('Cannot open network preferences');
function readSnapshot() {
    var value = $.SCPreferencesGetValue(prefs, $('NetworkServices'));
    if (!value) throw Error('Cannot read network services');
    var services = ObjC.deepUnwrap(ObjC.castRefToObject(value));
    var snapshot = {};
    Object.keys(services).forEach(function(id) {
        if (!services[id].__INACTIVE__) snapshot[id] = services[id].Proxies || null;
    });
    return snapshot;
}
`

func (m *Manager) readMacOS() (macSettings, error) {
	output, err := m.run("osascript", "-l", "JavaScript", "-e", macPreferences+"JSON.stringify(readSnapshot());")
	if err != nil {
		return nil, fmt.Errorf("读取系统代理失败: %w", err)
	}
	var settings macSettings
	if err := json.Unmarshal(output, &settings); err != nil || len(settings) == 0 {
		return nil, fmt.Errorf("系统代理快照格式无效")
	}
	return settings, nil
}

func (m *Manager) writeMacOS(settings macSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	script := `ObjC.import('SystemConfiguration');
ObjC.import('Security');
var authorization = Ref();
if ($.AuthorizationCreate(null, null, 0, authorization) !== 0) throw Error('Cannot create authorization');
var prefs = $.SCPreferencesCreateWithAuthorization(null, $('GoTun'), null, authorization[0]);
if (!prefs) throw Error('Cannot open authorized network preferences');
` + "var changes = " + string(data) + `;
if (!$.SCPreferencesLock(prefs, true)) throw Error('Cannot lock network preferences');
try {
    Object.keys(changes).forEach(function(id) {
        var path = $('/NetworkServices/' + id + '/Proxies');
        var service = $.SCPreferencesPathGetValue(prefs, $('/NetworkServices/' + id));
        // A removed service has no remaining settings to restore.
        if (!ObjC.deepUnwrap(ObjC.castRefToObject(service))) return;
        var ok;
        if (changes[id] === null) {
            var current = $.SCPreferencesPathGetValue(prefs, path);
            ok = !ObjC.deepUnwrap(ObjC.castRefToObject(current)) || $.SCPreferencesPathRemoveValue(prefs, path);
        } else {
            ok = $.SCPreferencesPathSetValue(prefs, path, $(changes[id]));
        }
        if (!ok) throw Error('Cannot update proxy settings');
    });
    if (!$.SCPreferencesCommitChanges(prefs)) throw Error('Cannot commit proxy settings');
    if (!$.SCPreferencesApplyChanges(prefs)) throw Error('Cannot apply proxy settings');
} finally {
    $.SCPreferencesUnlock(prefs);
    $.AuthorizationFree(authorization[0], 0);
}
`
	if _, err := m.run("osascript", "-l", "JavaScript", "-e", script); err != nil {
		return fmt.Errorf("写入系统代理失败: %w", err)
	}
	return nil
}

func (m *Manager) snapshotMacOS() (func() error, error) {
	settings, err := m.readMacOS()
	if err != nil {
		return nil, err
	}
	m.macBaseline = settings
	return func() error { return m.writeMacOS(settings) }, nil
}

func (m *Manager) enableMacOS() error {
	data, err := json.Marshal(m.macBaseline)
	if err != nil {
		return err
	}
	var settings macSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(m.httpAddr)
	number, _ := strconv.Atoi(port)
	for id, proxy := range settings {
		if proxy == nil {
			proxy = make(map[string]any)
			settings[id] = proxy
		}
		for _, kind := range []string{"HTTP", "HTTPS"} {
			proxy[kind+"Enable"], proxy[kind+"Proxy"], proxy[kind+"Port"], proxy[kind+"ProxyAuthenticated"] = 1, host, number, 0
		}
		proxy["ExceptionsList"] = []string{}
		proxy["ExcludeSimpleHostnames"] = 0
		proxy["ProxyAutoConfigEnable"], proxy["ProxyAutoDiscoveryEnable"] = 0, 0
		proxy["SOCKSEnable"] = 0
		if m.socksAddr != "" {
			shost, sport, _ := net.SplitHostPort(m.socksAddr)
			sp, _ := strconv.Atoi(sport)
			proxy["SOCKSEnable"], proxy["SOCKSProxy"], proxy["SOCKSPort"], proxy["SOCKSProxyAuthenticated"] = 1, shost, sp, 0
		}
	}
	return m.writeMacOS(settings)
}

func (m *Manager) clearMacOS() error {
	settings, err := m.readMacOS()
	if err != nil {
		return err
	}
	for _, proxy := range settings {
		if proxy == nil {
			continue
		}
		for _, field := range []string{"HTTPEnable", "HTTPSEnable", "SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
			proxy[field] = 0
		}
	}
	return m.writeMacOS(settings)
}
