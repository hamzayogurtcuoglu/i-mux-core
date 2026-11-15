package vtfixture

// Specs are the byte streams of the VT fixture suite (testdata/vt/*.in), kept in Go so their
// escape sequences stay readable. Running cmd/vtfixture writes each entry to testdata/vt/<name>
// and records what the pinned tmux shows for it in the matching .screen file.
var Specs = map[string]string{
	// C1.3: wide characters, combining marks, UTF-8, wrapping and tabs.
	"combining@30x10.in": "e\xcc\x81 a\xcc\x88\r\n" +
		"\xcc\x81x\r\n" +
		"family \xf0\x9f\x91\xa8\xe2\x80\x8d\xf0\x9f\x91\xa9\xe2\x80\x8d\xf0\x9f\x91\xa7 end\r\n" +
		"smile \xe2\x98\xba\xef\xb8\x8f end\r\n" +
		"key 1\xef\xb8\x8f\xe2\x83\xa3 end\r\n" +
		"flag \xf0\x9f\x87\xb9\xf0\x9f\x87\xb7\xf0\x9f\x87\xac\xf0\x9f\x87\xa7 end\r\n" +
		"thumb \xf0\x9f\x91\x8d\xf0\x9f\x8f\xbd end\r\n" +
		"jamo \xe1\x84\x80\xe1\x85\xa1\xe1\x86\xa8 end\r\n" +
		"fill \xe3\x85\xa4 end\r\n" +
		"zwj-alone \xe2\x80\x8d end",
	"scroll-history@10x3.in": "line0\r\nline1\r\nline2\r\nline3\r\nline4\r\nline5\r\nline6\r\nline7\r\nlast",
	"tabs@30x6.in": "a\tb\r\n" +
		"\t\tc\r\n" +
		"xxxxxxxxxx\tz\r\n" +
		"xxxxxxxxxxxxxxxxxxxxxxxxxxxx\ty\r\n" +
		"\xe6\x97\xa5\tq",
	"utf8-invalid@20x5.in": "a\xffb\r\nc\xc3d\r\ne\xe4\xc3\xa9f\r\ng\xed\xa0\x80h\r\ni\xc3\nj",
	"wide-basic@20x6.in": "a\xe6\x97\xa5\xe6\x9c\xacb\r\n" +
		"\xf0\x9f\x99\x82x\r\n" +
		"xxxxxxxxxxxxxxxxxxx\xe6\x97\xa5z\r\n" +
		"xxxxxxxxxxxxxxxxxx\xe6\x97\xa5\r\n" +
		"end",
	"wide-overwrite@20x6.in": "\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\ra\r\n" +
		"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\x08\x08x\r\n" +
		"\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\x08\x08\x08y\r\n" +
		"ab\xe6\x97\xa5\x08\x08\x08\x08\xe6\x97\xa5",
	"wrap@10x5.in": "0123456789abcdefghijk\r\nshort\r\n0123456789\r\nnext",

	// C1.4: cursor movement, erasing, editing, scrolling, tab stops, cursor save/modes and resets.
	"cursor-move@12x6.in": "0123456789ab\r\n" +
		"abcdefghijkl\r\n" +
		"mnopqrstuvwx\r\n" +
		"yz0123456789\r\n" +
		"............\r\n" +
		"------------" +
		"\x1b[H*" +
		"\x1b[2;4f#" +
		"\x1b[2B\x1b[3C@" +
		"\x1b[1A\x1b[2D!" +
		"\x1b[2E+" +
		"\x1b[1F=" +
		"\x1b[8G~" +
		"\x1b[10`^" +
		"\x1b[6d/" +
		"\x1b[3a\x1b[2e" +
		"\x1b[20h\n>\x1b[20l" +
		"\x08<" +
		"\x1b[99;99H$" +
		"\x1b[99A\x1b[99D%",
	"cursor-pending-wrap@10x5.in": "0123456789" +
		"\x1b[DX\r\n" +
		"0123456789" +
		"\x1b[CY\r\n" +
		"0123456789" +
		"\x08Z\r\n" +
		"0123456789" +
		"\x1b[?7l!\x1b[?7hW" +
		"\x1b[1;1H0123456789\x1b[B*",
	"erase-display@10x5.in": "AAAAAAAAAA\r\n" +
		"BBBBBBBBBB\r\n" +
		"CCCCCCCCCC\r\n" +
		"DDDDDDDDDD\r\n" +
		"EEEEEEEEEE" +
		"\x1b[2;4H\x1b[1J" +
		"\x1b[4;7H\x1b[0J",
	"erase-history@10x4.in": "line1\r\nline2\r\nline3\r\nline4\r\nline5\r\nline6" +
		"\x1b[2J" +
		"after2J\r\nsecond" +
		"\x1b[3;1J" +
		"\x1b[3J" +
		"\x1b[1;1H\x1b[J" +
		"fresh",
	"erase-line@10x6.in": "0123456789\r\n" +
		"abcdefghij\r\n" +
		"klmnopqrst\r\n" +
		"uvwxyz0123\r\n" +
		"456789abcd\r\n" +
		"efghijklmn" +
		"\x1b[1;5H\x1b[0K" +
		"\x1b[2;5H\x1b[1K" +
		"\x1b[3;5H\x1b[2K" +
		"\x1b[4;3H\x1b[4X" +
		"\x1b[5;1H0123456789\x1b[1K" +
		"\x1b[6;8H\x1b[99X",
	"edit-chars@12x6.in": "abcdefghijkl\r\n" +
		"0123456789ab\r\n" +
		"\x1b[1;4H\x1b[2@" +
		"\x1b[2;3H\x1b[3P" +
		"\x1b[3;1H\x1b[4hABCD\x1b[2D12\x1b[4l\r\n" +
		"X\x1b[4b!\x1b[99b\r\n" +
		"\xe6\x97\xa5\x1b[2b\r\n" +
		"\x1b(0q\x1b[3b\x1b(B+",
	"edit-lines@10x6.in": "row1\r\nrow2\r\nrow3\r\nrow4\r\nrow5\r\nrow6" +
		"\x1b[2;5r" +
		"\x1b[3;2H\x1b[1Linserted" +
		"\x1b[4;1H\x1b[1M" +
		"\x1b[1;1H\x1b[1Ltop" +
		"\x1b[6;1H\x1b[1M",
	"scroll-region@10x6.in": "L1........\r\n" +
		"L2........\r\n" +
		"L3........\r\n" +
		"L4........\r\n" +
		"L5........\r\n" +
		"L6........" +
		"\x1b[2;5r" +
		"\x1b[5;2r\x1b[3;3r" +
		"\x1b[2;3H\x1bMtopin" +
		"\x1b[5;1H\x1bDbotin" +
		"\x1b[2;1H\x1b[99A^" +
		"\x1b[5;2H\x1b[99Bv" +
		"\x1b[1;4H\x1b[99A!" +
		"\x1b[6;4H\x1b[99B?" +
		"\x1b[?6h\x1b[1;1H@\x1b[99;99H#\x1b[?6l" +
		"\x1b[1S\x1b[1T" +
		"\x1b[3;4H\x1bEnext",
	"tabs-stops@20x6.in": "\x1b[3g" +
		"\x1b[5G\x1bH\x1b[10G\x1bH\x1b[15G\x1bH" +
		"\rA\tB\tC\tD\r\n" +
		"\x1b[10G\x1b[0g" +
		"\r1\t2\t3\r\n" +
		"\x1b[18G\x1b[1Z*\x1b[2Z#\x1b[1I$",
	"save-reset@12x5.in": "before-reset\r\nsecond-line" +
		"\x1bc" +
		"\x1b#8" +
		"\x1b[2;4r\x1b[?6h\x1b[2;3H\x1b(0" +
		"\x1b7" +
		"\x1b[?6l\x1b(B\x1b[1;1HTOP" +
		"\x1b8" +
		"q\x1b(B!" +
		"\x1b[s\x1b[5;1HBOT\x1b[u*" +
		"\x1b[!p",

	// C1.5 SGR: colours and attributes.
	"sgr-attrs@24x6.in": "\x1b[1mB\x1b[2mD\x1b[22mN" +
		"\x1b[3mI\x1b[23mN" +
		"\x1b[4mU\x1b[24mN" +
		"\x1b[5mK\x1b[6mF\x1b[25mN\r\n" +
		"\x1b[7mR\x1b[27mN" +
		"\x1b[8mH\x1b[28mN" +
		"\x1b[9mS\x1b[29mN" +
		"\x1b[53mO\x1b[55mN\r\n" +
		"\x1b[1;2;3;4;5;7;8;9;53mALL\x1b[0mRST\r\n" +
		"\x1b[1;3;31mCOMBO\x1b[mPLAIN\r\n" +
		"\x1b[;1;;4mSEMI\x1b[0mEND",
	"sgr-colors@24x8.in": "\x1b[30m0\x1b[31m1\x1b[32m2\x1b[33m3\x1b[34m4\x1b[35m5\x1b[36m6\x1b[37m7\x1b[39mD\r\n" +
		"\x1b[90m0\x1b[91m1\x1b[92m2\x1b[93m3\x1b[94m4\x1b[95m5\x1b[96m6\x1b[97m7\x1b[39mD\r\n" +
		"\x1b[40m0\x1b[41m1\x1b[42m2\x1b[43m3\x1b[44m4\x1b[45m5\x1b[46m6\x1b[47m7\x1b[49mD\r\n" +
		"\x1b[100m0\x1b[101m1\x1b[102m2\x1b[103m3\x1b[104m4\x1b[105m5\x1b[106m6\x1b[107m7\x1b[49mD\r\n" +
		"\x1b[38;5;196;48;5;21mP256\x1b[38:5:46;48:5:235mC256\x1b[38;5;999mBAD\x1b[0m\r\n" +
		"\x1b[38;2;10;20;30;48;2;40;50;60mRGB1\x1b[0m\r\n" +
		"\x1b[38:2:70:80:90;48:2::100:110:120mRGB2\x1b[0m\r\n" +
		"\x1b[38:2:0:130:140:150:1:2mCS\x1b[38;2;300;1;2mBAD\x1b[0m!",
	"sgr-underline@24x6.in": "\x1b[4mU1\x1b[21mU2\x1b[4:0mU0\r\n" +
		"\x1b[4:1mS\x1b[4:2mD\x1b[4:3mC\x1b[4:4mO\x1b[4:5mH\x1b[24mN\r\n" +
		"\x1b[4:3;58;5;196mC256\x1b[58:5:82mC256B\x1b[59mDEF\r\n" +
		"\x1b[4:1;58;2;12;34;56mRGB1\x1b[58:2:78:90:123mRGB2\r\n" +
		"\x1b[58:2::200:100:50mRGB3\x1b[59mRST\x1b[0m!",
	"sgr-bce@12x5.in": "\x1b[44mAB\x1b[K\r\n" +
		"\x1b[42m012345\x1b[1;4H\x1b[2X\r\n" +
		"\x1b[3;1H\x1b[41mabcdef\x1b[3;3H\x1b[2@\x1b[3;8H\x1b[1P\r\n" +
		"\x1b[4;1H\x1b[43mXXXXXX\x1b[4;3H\x1b[1K\r\n" +
		"\x1b[5;1H\x1b[45mYYYY\x1b[0J\x1b[0m",

	// C1.6 Modes and the alternate screen.
	"altscreen-1049@12x5.in": "hist-line\r\n" +
		"main-1\r\nmain-2\r\nmain-3\r\nmain-4\r\nmain-5" +
		"\x1b[2;4H\x1b[32m" +
		"\x1b[?1049h" +
		"\x1b[1;1HALT-TOP\r\nALT-2\r\nALT-3\r\nALT-4\r\nALT-5\r\nALT-SCROLL" +
		"\x1b[31m" +
		"\x1b[?1049l" +
		"RESTORED\x1b[0m",
	"altscreen-stay@12x4.in": "p-hist\r\np-1\r\np-2\r\np-3\r\np-4" +
		"\x1b[?1049h\x1b[1;1H" +
		"alt-row-1\r\nalt-row-2\r\nalt-row-3\r\nalt-row-4\r\nalt-row-5",
	"altscreen-47@12x4.in": "keep-1\r\nkeep-2\r\nkeep-3\r\nkeep-4" +
		"\x1b[2;3H\x1b[?47h\x1b[1;1Htemp47\x1b[4;8H\x1b[?47l*" +
		"\x1b[?1047h\x1b[1;1Htemp1047\x1b[3;2H\x1b[?1047l#",
}
