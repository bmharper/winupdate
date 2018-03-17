# winupdate
winupdate is a minimalist and opinionated software update library for Windows applications.
Linux and OSX have good package systems, so we don't try to make this work for them.
winupdate tries to be as transparent as possible, so that your users are unaware that an update has occurred. You can obviously show them a "What's New" message if you want, but the update process itself is designed to be completely unnoticeable.

winupdate has only three small source files, and should be trivial to integrate into your program. Preparing an update involves producing a .zip file of your new program image, computing the sha256 hash of that file, and hosting those two files on an HTTP(s) server.

## How to use winupdate
1. Add winupdate.cpp and winupdate.h to your program. You can link them as a static library if you want,
or just include them into your project.
2. Download the latest version of Go, and `go build winupdate.go`, which will produce `winupdate.exe`.
3. At the earliest convenient place in your program, call `winupdate::Update(url)`, and take appropriate
action, depending on the return value (see example below).
4. Make your application install into `C:\Users\USER\AppData\Local\COMPANY\PRODUCT`
5. Include `winupdate.exe` with your application (in the root directory of your application)
6. Choose an HTTP(s) location to host your updates. Prepare your updates by creating a .zip file that contains a new, entire image of your program. Be sure to include `winupdate.exe` in the update image. Whatever is in the .zip file completely replaces your old application directory. Don't forget to include the .sha256 hash file of the entire .zip file, as discussed below. Optionally compress the .zip file with bzip2, as discussed below.
7. Test it. You will see update logs inside `C:\Users\USERS\AppData\Local\COMPANY\winupdate.log`, which should help you to debug any issues.

## How winupdate works
There is a Go program called `winupdate.exe`, which you ship in the same directory as your application.
Every time your program runs, the winupdate C++ function launches that Go program in the background, passing it the URL
of your update site. That URL points to a file that has been zipped, and then optionally also
compressed with bzip2.

The URL might look like https://example.com/myapp-next.zip.bz2

In addition to that URL, you must provide another that adds .sha256 to it, so for the above example,
that would be https://example.com/myapp-next.zip.bz2.sha256

The updater will download both of those, and if the hash matches, then it knows that an update
has been successfully downloaded, and is ready to be unpacked. The .sha256 file can be 32 bytes
of raw hash, or 64 characters hex encoded. If the file is 32 bytes exactly, then it is interpreted
as raw hash. If it is 64 characters or longer, then it is interpreted as hex encoded, and only the
first 64 bytes are consumed.

After downloading and verifying a new update, `winupdate.exe` will unpack the archive into
a temporary directory with the extension `-next`. In addition, `winupdate.exe` will place a copy
of itself inside a temporary directory with the extension `-temp`. The version from `-temp`
is invoked when performing an actual update, so that there are no locked binaries, and after
the update is finished, the `-next` directory can be deleted. The `-temp` directory remains
there, until the next update, at which time it is overwritten again with the latest copy of itself.

Your C++ application only calls a single function in all of this:

```cpp
#include <winupdate.h>

int APIENTRY WinMain(HINSTANCE hInstance, HINSTANCE hPrevInstance, LPSTR lpCmdLine, int nCmdShow) {
    if (winupdate::Update(L"https://example.com/myapp-next.zip") == winupdate::Action::ExitNow)
        return 0;
    // continue as usual
}
```

## Application layout
winupdate expects you to store your program in a location such as

    c:\Users\bob\AppData\Local\Company\Product

Where exactly you store your program does not matter, so long as you own the last two directories in that
hierarchy (ie the `Company\Product` portion)

winupdate is not built to handle updates to a location that requires UAC elevation. It wouldn't be particularly
hard to upgrade it, in order to do so. If you have a choice, then the seamless user experience of
unannounced upgrades is certainly the best.

## But what about delta compression?
It's just not worth it. If you distribute an application as popular as Chrome, or Windows itself,
then sure, it's worth it. But for most applications, it just doesn't matter. Delta compression
adds a ton of complexity.

## winupdate.exe adds 6 MB to my distribution?
That's just the tradeoff I decided to make. If C++ had a better standard library, then it would be
expedient to build it natively. However, Go gives you an HTTP client, zip, bzip2, sha256, filesystem traversal, out of the
box.

## bzip2
bzip2 compresses better than zip. In order to use it effectively, you must create the zip
archive with -0 compression, so that bzip2 compresses the raw stream, not already-zipped data, for example: 
`zip -0 MyAppUpdate.zip bin/* && bzip2 -9 MyAppUpdate.zip`