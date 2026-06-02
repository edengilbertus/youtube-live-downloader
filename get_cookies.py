import sys
import os

try:
    from pycookiecheat import chrome_cookies
except ImportError:
    print("Error: pycookiecheat is not installed.")
    print("Please install it by running: pip3 install pycookiecheat")
    sys.exit(1)

def main():
    try:
        cookies = chrome_cookies('https://www.youtube.com')
        if not cookies:
            print("No YouTube cookies found in Chrome. Make sure you are logged into YouTube in Chrome.")
            sys.exit(1)
        
        output_path = 'cookies.txt'
        if len(sys.argv) > 1:
            output_path = sys.argv[1]
            
        with open(output_path, 'w') as f:
            f.write('# Netscape HTTP Cookie File\n')
            f.write('# Generated automatically by get_cookies.py\n\n')
            for c in cookies.values():
                secure_str = 'TRUE' if c.secure else 'FALSE'
                expires = c.expires if c.expires is not None else 0
                f.write(f"{c.domain}\tTRUE\t{c.path}\t{secure_str}\t{expires}\t{c.name}\t{c.value}\n")
        print(f"Successfully exported {len(cookies)} cookies to {output_path}!")
    except Exception as e:
        print(f"Error extracting cookies: {e}")
        print("\nNote: On macOS, your terminal application (e.g. Terminal, iTerm) might need 'Full Disk Access'")
        print("enabled in System Settings -> Privacy & Security -> Full Disk Access to read Chrome's local database.")
        sys.exit(1)

if __name__ == '__main__':
    main()
