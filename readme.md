# winupdate
winupdate is a minimalist and opinionated software update library for Windows applications.
Linux and OSX have good package systems, so we don't try to make this work for them.

## How winupdate works
There is a Go program called `winupdate.exe`, which you ship in the same directory as your application.
Every time your program runs, you launch that program in the background, passing it the URL
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
there, until the next update, when it overwritten again with the latest copy of itself.

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

## winupdate.exe is seven megs?
That's just the tradeoff I decided to make. If C++ had a better standard library, then it would be
expedient to build it natively. However, Go gives you an HTTP client, zip, bzip2, sha256, filesystem traversal, out of the
box.

## bzip2
bzip2 can yield better compression than zip. In order to use it effectively, you must create the zip
archive with -0 compression, so that bzip2 compresses the raw stream, not already-zipped data, for example: 
`zip -0 MyAppUpdate.zip bin/* && bzip2 -9 MyAppUpdate.zip`