#pragma once

namespace winupdate {

enum class Action {
	ContinueAsUsual,
	ExitNow,
};

// Call this at program startup. If the return value is ExitNow, then you must exit immediately.
// archiveURL is your update archive, for example https://example.com/windows/myprogram-update.zip.bz2
Action Update(const wchar_t* archiveURL);

} // namespace winupdate