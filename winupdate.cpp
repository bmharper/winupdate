#include <windows.h>
#include <stdio.h>
#include <string>
#include <string.h>
#include "winupdate.h"

using namespace std;

namespace winupdate {

// Retrieve directory of path, which is everything up to the last backslash (but excluding the last backslash)
static std::wstring Dir(std::wstring path) {
	auto lastSlash = path.rfind('\\');
	if (lastSlash == -1)
		return L"";
	else
		return path.substr(0, lastSlash);
}

// Returns everything after the last backslash, or the entire string if there are no backslashes
static std::wstring Basename(std::wstring path) {
	auto lastSlash = path.rfind('\\');
	if (lastSlash == -1)
		return path;
	else
		return path.substr(lastSlash + 1);
}

// Retrieve full path of current executing process
static std::wstring AppPath() {
	wchar_t buf[4096];
	GetModuleFileNameW(NULL, buf, 4096);
	buf[4095] = 0;
	return buf;
}

// Retrieve directory of current executing process
static std::wstring AppDir() {
	return Dir(AppPath());
}

static std::wstring BuildSpecialDir(std::wstring extension) {
	auto appDir   = AppDir();
	auto oneAbove = Dir(appDir);
	auto appName  = appDir.substr(oneAbove.size());
	return oneAbove + L"\\" + appName + extension;
}

// C:\Users\bob\AppData\Local\Company\Product-next
static std::wstring NextUpdateDir() {
	return BuildSpecialDir(L"-next");
}

// C:\Users\bob\AppData\Local\Company\Product-temp
static std::wstring TempDir() {
	return BuildSpecialDir(L"-temp");
}

static bool Launch(wstring cmd) {
	wchar_t* buf = new wchar_t[cmd.size() + 1];
	wcscpy_s(buf, cmd.size() + 1, cmd.c_str());
	STARTUPINFOW        startInfo;
	PROCESS_INFORMATION procInfo;
	memset(&startInfo, 0, sizeof(startInfo));
	memset(&procInfo, 0, sizeof(procInfo));
	startInfo.cb  = sizeof(startInfo);
	bool launchOK = !!CreateProcessW(nullptr, buf, nullptr, nullptr, false, DETACHED_PROCESS, nullptr, nullptr, &startInfo, &procInfo);
	delete[] buf;
	CloseHandle(procInfo.hProcess);
	CloseHandle(procInfo.hThread);
	return launchOK;
}

// Create a system-wide semaphore that signals to other processes, that there is at least one of us running.
// This is necessary, because it is impossible to update an application when there are other copies of it
// running. To put it another way, this is used to detect if we are the first instance of this program to run,
// and only if that is true, do we proceed with an update.
// This function returns true if we are the first instance to run.
static bool IsFirstInstance() {
	// We never lock the mutex. We are merely using it's presence to detect if there are other versions of us running.
	auto name = AppDir();
	for (size_t i = 0; i < name.size(); i++) {
		if (name[i] == '\\')
			name[i] = '_';
	}
	// the Go side of us also has a runner lock, called "winupdate-runner-lock-" + ...
	name         = L"winupdate-self-lock" + name;
	HANDLE mutex = CreateMutexW(nullptr, true, name.c_str());
	if (mutex == nullptr)
		return false;
	if (GetLastError() == ERROR_ALREADY_EXISTS)
		return false;
	return true;
}

static bool IsUpdateReady() {
	return GetFileAttributesW((NextUpdateDir() + L"\\update.ready").c_str()) != INVALID_FILE_ATTRIBUTES;
}

static void LaunchUpdateDownloader(const wchar_t* archiveURL) {
	Launch(L"\"" + AppDir() + L"\\winupdate.exe\" download " + archiveURL);
}

Action Update(const wchar_t* archiveURL) {
	if (!IsFirstInstance())
		return Action::ContinueAsUsual;

	// Regardless of whether we are going to update or not, we leave the mutex created, so that
	// other processes know we're still alive.

	if (!IsUpdateReady()) {
		LaunchUpdateDownloader(archiveURL);
		return Action::ContinueAsUsual;
	}

	auto self = Basename(AppPath());

	if (Launch(L"\"" + TempDir() + L"\\winupdate.exe\" update \"" + self + L"\""))
		return Action::ExitNow;

	return Action::ContinueAsUsual;
}

} // namespace winupdate