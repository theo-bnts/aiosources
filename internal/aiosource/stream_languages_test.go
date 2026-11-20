package aiosource

import (
	"slices"
	"strings"
	"testing"

	"github.com/theo-bnts/aiosources/internal/model"
)

func TestInternationalTitleAudioAndSubtitleDetection(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		wantAudio []string
		wantSubs  []string
	}{
		// French scene/P2P tags (including VFI, VOF, VFSTFR, yearless INTEGRALE / technical boundaries).
		{"french vfi multi", "Example.Film.2001.MULTI.VFI.1080p.BLURAY.x264-GRP", []string{"multi", "fr"}, nil},
		{"french vof", "Example.Film.2008.VOF.1080p.WEB.EAC3.5.1.H265-GRP", []string{"fr"}, nil},
		{"french vfstfr", "Example.Film.2022.VFSTFR.1080p.WEB.H264-GRP", []string{"fr"}, []string{"fr"}},
		{"french yearless before 720p", "Example.Series.FRENCH.720p.WEB.H264-GRP", []string{"fr"}, nil},
		{"french integrale before 720p", "Example.Series.INTEGRALE.FRENCH.720p.AC3.2.0.x264", []string{"fr"}, nil},
		{"french integrale sd webrip", "Example.Series.INTEGRALE.FRENCH.SD.WEBRip.AAC.2.0.x264-GRP", []string{"fr"}, nil},
		{"french integrale vf xvid", "Example.Series.INTEGRALE.VF.SD.MP3.2.0.XviD", []string{"fr"}, nil},
		{"french vostfr subtitle only", "Example.Series.S01E05.VOSTFR.1080p.WEB.H264-GRP", nil, []string{"fr"}},
		{"french subfrench subtitle only", "Example.Anime.S01E07.SUBFRENCH.1080p.CR.WEB-DL.H264-GRP", nil, []string{"fr"}},

		// Spanish & Latin American releases.
		{"spanish castellano", "Example.Film.2023.Castellano.1080p.BluRay.x265-GRP", []string{"es"}, nil},
		{"spanish dual latino", "Example.Film.2024.1080p.WEB-DL.Dual.Latino-GRP", []string{"multi", "es"}, nil},
		{"spanish temporada yearless", "Example.Series.Temporada.1.SPANISH.1080p.WEB-DL", []string{"es"}, nil},
		{"spanish vose subtitle only", "Example.Film.2024.VOSE.1080p.WEB-DL.H264-GRP", nil, []string{"es"}},
		{"spanish sub esp subtitle only", "Example.Film.2024.1080p.WEB-DL.SUB.ESP", nil, []string{"es"}},

		// German releases.
		{"german staffel yearless", "Example.Series.Staffel.1.GERMAN.1080p.BluRay.x264", []string{"de"}, nil},
		{"german dub anywhere", "Example.Anime.GerDub.720p.WEB-DL", []string{"de"}, nil},
		{"german sub only", "Example.Film.2024.1080p.GerSub.x264", nil, []string{"de"}},

		// Italian releases.
		{"italian stagione yearless", "Example.Series.Stagione.2.ITALIAN.1080p.WEB-DL", []string{"it"}, nil},
		{"italian sub ita only", "Example.Film.2024.1080p.WEB-DL.Sub.ITA", nil, []string{"it"}},
		{"italian subita compound", "Example.Film.2024.1080p.SUBITA.H264", nil, []string{"it"}},

		// Portuguese / Brazilian releases.
		{"portuguese dublado dual", "Example.Film.2024.1080p.WEB-DL.DUAL.Dublado.x264", []string{"multi", "pt"}, nil},
		{"portuguese pt-br tag", "Example.Film.2024.1080p.WEB-DL.[PT-BR].H265", []string{"pt"}, nil},
		{"portuguese legendado sub only", "Example.Film.2024.1080p.WEB-DL.Legendado.x264", nil, []string{"pt"}},

		// Polish releases.
		{"polish lektor", "Example.Film.2024.LEKTOR.PL.1080p.WEB-DL.x264", []string{"pl"}, nil},
		{"polish napisy sub only", "[SubGroup] Example Anime S01 [1080p CR WEB-DL H.264 AAC] [Napisy PL]", nil, []string{"pl"}},

		// Japanese, Korean, Chinese & Anime releases.
		{"anime dual audio en+ja", "[Group] Example Anime - S01E01 [Bluray-1080p][AV1][EN+JA][Dual Audio]", []string{"en", "ja", "multi"}, nil},
		{"anime eng sub only", "[Group] Example Anime - 05 [1080p WEB-DL][English Sub]", nil, []string{"en"}},
		{"anime english dub", "[Group] Example Anime - S01E04 [English Dub] [CR WEB-DL 720p H264]", []string{"en"}, nil},
		{"chinese mandarin cantonese", "Example.Film.2024.1080p.BluRay.Mandarin.Cantonese.x265", []string{"zh"}, nil},
		{"chinese cjk audio and hardsub", "[Group] Example S01 [WETV.WEB-DL 1080P AVC, M-SUB, HARD SUB (ZH-HANS)]", nil, []string{"multi", "zh"}},
		{"chinese native dub", "Example.Film.2024.国语.中字.1080p.WEB-DL", []string{"zh"}, []string{"zh"}},
		{"chinese softsub tag", "[Group] Example S01 (WebRip 1080p AV1) / PT-BR / Sub Esp / VOSTFR / 简繁内封", []string{"pt"}, []string{"es", "fr", "zh"}},
		{"korean native", "Example.Drama.2024.S01E01.한국어.1080p.WEB-DL", []string{"ko"}, nil},
		{"english vosta sub only", "Example.Film.2024.VOSTA.2160p.ZEE5.WEB-DL.Bengali.DDP5.1.H.265-GRP", []string{"bn"}, []string{"en"}},
		{"vietnamese vietsub", "Example.Film.2024.1080p.WEB-DL.Vietsub.x264", nil, []string{"vi"}},
		{"prefix subtitle list after dual audio", "Example (2025) WEBRip 1080p x264 EAC3 JPN THA SUB ITA ENG GER SPA FRE - Grp", []string{"ja", "th"}, []string{"it", "en", "de", "es", "fr"}},
		{"prefix single sub preserves preceding audio", "Example (2024) WEBRip 1080p x264 EAC3 ITA KOR SUB ITA - Grp", []string{"it", "ko"}, []string{"it"}},
		{"parenthesized multilingual dubs and subs lists", "[Group] Example - S01 (BD 1080p x265) [Multi-Audio] (Japanese, Russian/Русский Dubs) (English, Russian/Русский, German/Deutsch Subs)", []string{"multi", "ja", "ru"}, []string{"en", "ru", "de"}},
		{"streaming service between language and web-dl", "Example.Show.S01E01.GERMAN.AMZN.WEB-DL.1080p.H264-GRP", []string{"de"}, nil},
		{"french sd web before aac", "Example.Show.S01E01.FRENCH.SD.WEB.AAC.2.0.H264", []string{"fr"}, nil},
		{"french regional streaming tag before 1080p", "Example.Show.S10E35.FRENCH.TF1P.1080p.WEB.H264-GRP", []string{"fr"}, nil},
		{"multi 3d sbs movie after year", "Example.Film.2012.MULTI.3D.SBS.1080p.BluRay.H264.AC3.5.1-GRP", []string{"multi"}, nil},
		{"year bounded titlecase languages before resolution", "Example.2022.VOSTFR.Telugu-Hindi.2160p.WEBRIP-NOTAG", []string{"te", "hi"}, []string{"fr"}},

		// Indian, Middle Eastern, Turkish, Eastern European, Nordic & Dutch releases.
		{"indian multi audio", "Example.Movie.2024.1080p.WEB-DL.Hindi.Tamil.Telugu.Malayalam.x265", []string{"hi", "ta", "te", "ml"}, nil},
		{"turkish dublaj", "Example.Film.2024.1080p.TR.Dublaj.WEB-DL", []string{"tr"}, nil},
		{"turkish altyazi sub only", "Example.Film.2024.1080p.Altyazi.WEB-DL", nil, []string{"tr"}},
		{"czech slovak dabing", "Example.Film.2024.1080p.CZ.SK.WEB-DL", []string{"cs", "sk"}, nil},
		{"russian cyrillic", "Example.Film.2024.1080p.Русский.WEB-DL", []string{"ru"}, nil},
		{"ukrainian ua", "Example.Film.2024.1080p.UA.WEB-DL", []string{"uk"}, nil},
		{"dutch vlaams", "Example.Film.2024.1080p.Vlaams.WEB-DL", []string{"nl"}, nil},
		{"nordic swedish sub", "Example.Film.2024.1080p.SWESUB.BluRay.x264", nil, []string{"sv"}},
		{"persian farsi", "Example.Film.2024.1080p.Persian.WEB-DL", []string{"fa"}, nil},
		{"greek gr", "Example.Film.2024.1080p.GR.WEB-DL", []string{"el"}, nil},
		{"arabic sub only", "Example.Film.2024.1080p.ARSUB.WEB-DL", nil, []string{"ar"}},
		{"yearless german before 1080p", "Example.Movie.GERMAN.1080p.BluRay.x264", []string{"de"}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			audio, subs := streamLanguageEvidence(model.AIORecord{Title: tc.title})
			gotAudio := codesOf(audio)
			gotSubs := codesOf(subs)
			if !slices.Equal(gotAudio, tc.wantAudio) {
				t.Errorf("audio(%q) = %v, want %v", tc.title, gotAudio, tc.wantAudio)
			}
			if !slices.Equal(gotSubs, tc.wantSubs) {
				t.Errorf("subs(%q) = %v, want %v", tc.title, gotSubs, tc.wantSubs)
			}
		})
	}
}

func TestStructuredMultilingualFieldsAndCLDRNames(t *testing.T) {
	tests := []struct {
		name      string
		languages []string
		subs      []string
		wantAudio []string
		wantSubs  []string
	}{
		{"french multi included", []string{"Multi (Français inclus)"}, nil, []string{"multi", "fr"}, nil},
		{"french vff truefrench", []string{"Français (VFF/TrueFrench)"}, nil, []string{"fr"}, nil},
		{"french multi vf2 fr qc", []string{"Multi VF2 (FR + QC inclus)"}, nil, []string{"multi", "fr"}, nil},
		{"french quebecois vfq", []string{"Québecois (VFQ/French)"}, nil, []string{"fr"}, nil},
		{"french vostfr + japonais", []string{"VOSTFR", "JAPONAIS"}, nil, []string{"ja"}, []string{"fr"}},
		{"french anglais + vostfr", []string{"ANGLAIS", "VOSTFR"}, nil, []string{"en"}, []string{"fr"}},
		{"french multi vff anglais", []string{"MULTI", "VFF", "ANGLAIS"}, nil, []string{"multi", "fr", "en"}, nil},
		{"french vfstfr", []string{"VFSTFR"}, nil, []string{"fr"}, []string{"fr"}},
		{"french muet", []string{"Muet"}, nil, nil, nil},
		{"spanish structured names", []string{"Español (Latinoamérica)", "Inglés", "Japonés"}, []string{"Francés"}, []string{"es", "en", "ja"}, []string{"fr"}},
		{"german structured names", []string{"Deutsch", "Französisch", "Japanisch"}, []string{"Englisch"}, []string{"de", "fr", "ja"}, []string{"en"}},
		{"italian structured names", []string{"Italiano", "Giapponese", "Tedesco"}, nil, []string{"it", "ja", "de"}, nil},
		{"portuguese structured names", []string{"Português (Brasil)", "Inglês"}, []string{"Espanhol"}, []string{"pt", "en"}, []string{"es"}},
		{"dutch structured names", []string{"Nederlands", "Pools", "Duits"}, nil, []string{"nl", "pl", "de"}, nil},
		{"polish structured names", []string{"Polski", "Angielski", "Japoński"}, nil, []string{"pl", "en", "ja"}, nil},
		{"russian cyrillic names", []string{"Русский", "Английский", "Японский"}, nil, []string{"ru", "en", "ja"}, nil},
		{"cjk arabic native names", []string{"日本語", "한국어", "中文", "العربية"}, nil, []string{"ja", "ko", "zh", "ar"}, nil},
		{"hungarian cldr names", []string{"Magyar", "Olasz", "Német"}, nil, []string{"hu", "it", "de"}, nil},
		{"multi-word cldr name", []string{"Brazilian Portuguese", "Simplified Chinese"}, nil, []string{"pt", "zh"}, nil},
		{"phrase with french preposition en", []string{"Audio en français"}, nil, []string{"fr"}, nil},
		{"subtitle phrase inside languages", []string{"Subtitles: English, French"}, nil, nil, []string{"en", "fr"}},
		{"non-language phrases stay empty", []string{"not a language", "No dialogue", "unknown", "und", "VO"}, nil, nil, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			audio, subs := streamLanguageEvidence(model.AIORecord{
				Title:             "Example.Movie.2024.1080p",
				Languages:         tc.languages,
				SubtitleLanguages: tc.subs,
			})
			if got := codesOf(audio); !slices.Equal(got, tc.wantAudio) {
				t.Errorf("audio(%v) = %v, want %v", tc.languages, got, tc.wantAudio)
			}
			if got := codesOf(subs); !slices.Equal(got, tc.wantSubs) {
				t.Errorf("subs(%v, %v) = %v, want %v", tc.languages, tc.subs, got, tc.wantSubs)
			}
		})
	}
}

func TestInlineTagsSeparateAudioFromSubtitles(t *testing.T) {
	rec := model.AIORecord{
		Title: "[Group] Example Anime 2024 S03E03 1080p NF WEB-DL MULTi AAC2.0 H.264 (Multi-Audio, Multi-Subs) {Tags:L0;V9;C1;A=ja,en,frfr,de,ptbr,es419,eses;S=en,ar,zhhans,zhhant,pl,tr,vi;}",
	}
	audio, subs := streamLanguageEvidence(rec)
	wantAudio := []string{"ja", "en", "fr", "de", "pt", "es", "multi"}
	wantSubs := []string{"en", "ar", "zh", "pl", "tr", "vi", "multi"}
	if got := codesOf(audio); !slices.Equal(got, wantAudio) {
		t.Fatalf("inline tags audio = %v, want %v", got, wantAudio)
	}
	if got := codesOf(subs); !slices.Equal(got, wantSubs) {
		t.Fatalf("inline tags subs = %v, want %v", got, wantSubs)
	}

	fansub := model.AIORecord{
		Title: "[FanSub] Example S01 [1080p CR WEB-DL H.264 AAC] [Napisy PL] {Tags:OTL;L3;V9;C1;A=ja;F=pl;}",
	}
	fAudio, fSubs := streamLanguageEvidence(fansub)
	if got := codesOf(fAudio); !slices.Equal(got, []string{"ja"}) {
		t.Fatalf("fansub audio = %v, want [ja]", got)
	}
	if got := codesOf(fSubs); !slices.Equal(got, []string{"pl"}) {
		t.Fatalf("fansub subs = %v, want [pl]", got)
	}
}

func TestLanguageParserAvoidsCommonFalsePositives(t *testing.T) {
	tests := []struct {
		name      string
		title     string
		wantAudio []string
		wantSubs  []string
	}{
		{"he-aac is not hebrew", "Example.2024.S01.COMPLETE.MULTi.VFF.1080p.WEB.HE-AAC2.0.H264-GRP", []string{"multi", "fr"}, nil},
		{"dts-es is not spanish", "Example.2006.MULTI.VFF.1080p.BLURAY.REMUX.DTS.ES.5.1.AVC-GRP", []string{"multi", "fr"}, nil},
		{"dts-hd-hr is not croatian", "Example.2022.MULTI.VF2.2160p.Bluray.Remux.DTS-HD-HR.7.1.HEVC-GRP", []string{"multi", "fr"}, nil},
		{"no-vf is neither norwegian nor french", "Example.S01E07.MULTI.NO-VF.MULTISUB.1080p.WEBRip.H265-GRP", []string{"multi"}, []string{"multi"}},
		{"hi-res is not hindi", "Example.Album.2021.HI-Res.WEB.FLAC.24BIT", nil, nil},
		{"trailing 2-letter release group nl", "Example.1989.VOF.1080p.HDLIGHT.AAC.2.0.AVC-NL", []string{"fr"}, nil},
		{"trailing release group sr-71", "Example.S01.S02.INTEGRALE.VFF.1080p.BluRay.Opus.2.0.x265-SR-71", []string{"fr"}, nil},
		{"trailing bracketed release group sr-71 with localized title", "Example.2017.iNTEGRALE.MULTi.VFF.1080p.10bit.DDP2.0.x265-[SR-71] (Alt.Title)", []string{"multi", "fr"}, nil},
		{"trailing release groups ind aze ben ron gr fas irish dutch", "Example.2024.MULTi.VFF.1080p.BluRay.x264-iND", []string{"multi", "fr"}, nil},
		{"trailing release group ben.the.men", "Example.2025.MULTi.2160p.UHD.BluRay.Remux.H265-BEN.THE.MEN", []string{"multi"}, nil},
		{"trailing release group ron", "Example.2023.CUSTOM.MULTi.1080p.BluRay.REMUX.AVC.TrueHD.7.1.Atmos-RoN", []string{"multi"}, nil},
		{"trailing release group aze", "Example.2018.MULTi.TRUEFRENCH.1080p.BluRay.AC3.x265-AZE", []string{"multi", "fr"}, nil},
		{"trailing release group irish", "Example.S01.FRENCH.SD.WEBRip.AAC.2.0.x264-irish", []string{"fr"}, nil},
		{"trailing localized french title Vie and fin", "Example.1994.iNTEGRALE.FRENCH.VFF.720p.WEB.AAC.x264-GRP (La Vie et la fin)", []string{"fr"}, nil},
		{"trailing localized title with Roman numeral VI", "Star.Trek.VI.1991.MULTI.1080p.HEVC.x265-GRP (Star Trek VI - Terre inconnue)", []string{"multi"}, nil},
		{"leading bracket prefix before title", "[S5.E1] The IT Crowd - Final Episode", nil, nil},
		{"romaji no in parenthetical alt title", "[Group] Example - S01E04 v2 [English Dub] [CR WEB-DL 720p H264] (Hotaru no Yomeiri)", []string{"en"}, nil},
		{"pipe alternative title with Ru", "[Group] Example Season 2 (BD 1080p x264) [Dual Audio] | Motto To Love Ru", []string{"multi"}, nil},
		{"french preposition de in title", "Example.T01.1869.La.conquete.de.l.espace.2014.[CBZ]-NOTAG", nil, nil},
		{"movie title with language before year", "In.French.2024.1080p.WEB-DL", nil, nil},
		{"titlecase language without year", "Learning.English.1080p.WEB-DL.x264", nil, nil},
		{"titlecase italian without year", "The.Italian.Job.1080p.BluRay.x264", nil, nil},
		{"dual layer dvd is not multi", "Example.2024.1080p.Dual.Layer.DVD", nil, nil},
		{"mal id is not malayalam", "Example.Anime.2024.1080p.MAL.12345.WEB-DL", nil, nil},
		{"roman word is not romanian", "Example.2024.S01E01.Roman.Holiday.1080p.WEB-DL", nil, nil},
		{"episode title welsh between s03e01 and 1080p", "Example.Show.S03E01.Welsh.Custard.1080p.AMZN.WEB-DL.H264-GRP", nil, nil},
		{"episode title basque before date and french", "Example.S01E02.Pays.basque.2014-04-05.FRENCH.AD.1080p.WEB.H264-GRP", []string{"fr"}, nil},
		{"episode title khmer before year and doc french", "Example.S07E04.Empire.khmer.2022.DOC.FRENCH.1080p.WEB.H264-GRP", []string{"fr"}, nil},
		{"episode title car model slk before french", "Example.S09E25.Mercedes.SLK.200.K.FRENCH.720p.WEB.H264-GRP", []string{"fr"}, nil},
		{"episode title car model sl before year", "Example.S05E01.Mercedes.230.SL.2013.Webrip.720p", nil, nil},
		{"all-caps romaji particle NO before subfrench", "Example.2010.OAV.3.WATASHI.NO.KOKORO.SUBFRENCH.FANSUB.1080p.X264", nil, []string{"fr"}},
		{"roman numeral VI after integrale before french", "Example.Integrale (Livres I a VI + Bonus) FRENCH [1080p] BDRIP HEVC-GRP", []string{"fr"}, nil},
		{"japanese kan subtitle word is not kannada", "[SR-71] EXAMPLE.TITLE.KAN.VOSTFR.BLURAY.[1080P][X265]", nil, []string{"fr"}},
		{"dotted sr-71 suffix is not serbian", "Example.S01.2019.VOSTFR.1080p.OPUS.2.0.X265.10BITS.SR-71", nil, []string{"fr"}},
		{"ddp5.1-ro trailing release group", "Example.2000.SUBFRENCH.1080p.BluRay.x264.DDP5.1-RO", nil, []string{"fr"}},
		{"dd5.1-diy-gr trailing release group", "Example.2007.1080p.Remux.VC-1.DD5.1-DIY-GR", nil, nil},
		{"multi-hyphen hr-dr and bracketed hr release group", "Example.2017.MULTI.VFF.1080p.WEB.OPUS.H265-HR-DR", []string{"multi", "fr"}, nil},
		{"spaced bracketed hr release group", "Example - Saison 01 - MULTiSUBS - 1080p - HEVC - OPUS - [HR]", nil, []string{"multi"}},
		{"dotted irish domain release group", "Example.2021.S05E08.FASTSUB.VOSTFR.1080p.WEB.H264-Site.irish", nil, []string{"fr"}},
		{"no-tag is not norwegian", "Example.2024.MULTi.1080p.WEB.x265.10bit.AC3.AAC.NO-TAG", []string{"multi"}, nil},
		{"flac 1.0-sr release group is not serbian", "Example.1958.VOST.1080p.BluRay.REMUX.AVC.FLAC.1.0-SR", nil, nil},
		{"x264-srp release group is not serbian", "Example.2012.1080p.BluRay.HDLight.DTS.5.1.x264-SRP", nil, nil},
		{"bracketed scc fansub group is not serbian", "Example VOSTFR 1080P H264 AAC [SCC]", nil, []string{"fr"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			audio, subs := streamLanguageEvidence(model.AIORecord{Title: tc.title})
			if got := codesOf(audio); !slices.Equal(got, tc.wantAudio) {
				t.Errorf("audio(%q) = %v, want %v", tc.title, got, tc.wantAudio)
			}
			if got := codesOf(subs); !slices.Equal(got, tc.wantSubs) {
				t.Errorf("subs(%q) = %v, want %v", tc.title, got, tc.wantSubs)
			}
		})
	}
}

func TestPlaybackPresentationReadsSelectedFilenameLanguages(t *testing.T) {
	rec := model.AIORecord{
		Provider: "source",
		Hash:     strings.Repeat("b", 40),
		Size:     20_000_000_000,
		Title:    "Example.Series.S01.COMPLETE.1080p.WEB-DL",
	}
	base := baseStream(rec, Resource{Type: "series"}, "Example")
	cached := true
	stream := playbackStream(base, rec, "alldebrid", &cached, &model.AIOFile{
		Path: "Example.Series.S01/Example.Series.S01E01.MULTi.VFF.VOSTFR.1080p.WEB-DL.mkv",
		Size: 2_000_000_000,
	}, "https://files.example/ep1")
	if !strings.Contains(stream.Name, "French") || !strings.Contains(stream.Name, "🇫🇷") || !strings.Contains(stream.Name, "French subtitles") {
		t.Fatalf("selected file languages/subtitles not reflected in presentation: %q", stream.Name)
	}
}

func TestOriginalLanguageInferenceForVOAndSubtitleReleases(t *testing.T) {
	tests := []struct {
		name      string
		rec       model.AIORecord
		wantAudio []string
		wantSubs  []string
	}{
		{
			name:      "japanese anime VOSTFR infers ja audio + fr subs",
			rec:       model.AIORecord{Title: "Example.Anime.S01E01.VOSTFR.1080p.WEB-DL.x264-GRP", OriginalLanguage: "ja"},
			wantAudio: []string{"ja"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "english movie SUBFRENCH infers en audio + fr subs",
			rec:       model.AIORecord{Title: "Example.Movie.2024.SUBFRENCH.1080p.BluRay.x264-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"en"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "english movie STFR infers en audio + fr subs",
			rec:       model.AIORecord{Title: "Example.Movie.2024.STFR.1080p.WEB-DL.H264-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"en"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "french movie ENGSUB infers fr audio + en subs",
			rec:       model.AIORecord{Title: "Example.Film.2023.1080p.BluRay.EngSub.x264-GRP", OriginalLanguage: "fr"},
			wantAudio: []string{"fr"},
			wantSubs:  []string{"en"},
		},
		{
			name:      "korean movie VOSE infers ko audio + es subs",
			rec:       model.AIORecord{Title: "Example.Movie.2019.VOSE.1080p.BluRay.x264-GRP", OriginalLanguage: "ko"},
			wantAudio: []string{"ko"},
			wantSubs:  []string{"es"},
		},
		{
			name:      "german OmU infers original language without named sub",
			rec:       model.AIORecord{Title: "Example.Movie.2022.OmU.1080p.BluRay.x264-GRP", OriginalLanguage: "ja"},
			wantAudio: []string{"ja"},
			wantSubs:  nil,
		},
		{
			name:      "german OmEU infers original language + en subs",
			rec:       model.AIORecord{Title: "Example.Movie.2022.OmEU.1080p.BluRay.x264-GRP", OriginalLanguage: "de"},
			wantAudio: []string{"de"},
			wantSubs:  []string{"en"},
		},
		{
			name:      "german OV marker infers original language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.OV.1080p.WEB-DL-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"en"},
			wantSubs:  nil,
		},
		{
			name:      "french VO marker in title infers original language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.VO.1080p.WEB-DL-GRP", OriginalLanguage: "it"},
			wantAudio: []string{"it"},
			wantSubs:  nil,
		},
		{
			name:      "structured VO + sublangs infers original language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL-GRP", Languages: []string{"VO"}, SubtitleLanguages: []string{"fr"}, OriginalLanguage: "en"},
			wantAudio: []string{"en"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "structured Version Originale phrase infers original language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL-GRP", Languages: []string{"Version Originale"}, OriginalLanguage: "ja"},
			wantAudio: []string{"ja"},
			wantSubs:  nil,
		},
		{
			name:      "international subbed markers legendado napisy altyazi",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL.Legendado-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"en"},
			wantSubs:  []string{"pt"},
		},
		{
			name:      "tmdb cantonese cn maps to zh",
			rec:       model.AIORecord{Title: "Example.Movie.2000.VOSTFR.1080p.BluRay-GRP", OriginalLanguage: "cn"},
			wantAudio: []string{"zh"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "propagated subtitle from peer unlocks original language inference",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.BluRay.x264-GRP", PropagatedSubtitleLanguages: []string{"fr"}, OriginalLanguage: "ja"},
			wantAudio: []string{"ja"},
			wantSubs:  []string{"fr"},
		},
		// Strict zero-false-positive guards:
		{
			name:      "VFSTFR already has French audio and never adds original_language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.VFSTFR.1080p.WEB-DL-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"fr"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "explicit dub + subtitle never adds original_language",
			rec:       model.AIORecord{Title: "Example.Anime.2024.FRENCH.SUBENG.1080p.WEB-DL-GRP", OriginalLanguage: "ja"},
			wantAudio: []string{"fr"},
			wantSubs:  []string{"en"},
		},
		{
			name:      "MULTI + VOSTFR keeps MULTI without overriding",
			rec:       model.AIORecord{Title: "Example.Movie.2024.MULTI.VOSTFR.1080p.WEB-DL-GRP", OriginalLanguage: "en"},
			wantAudio: []string{"multi"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "cross-source propagated audio takes precedence over TMDB original_language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.VOSTFR.1080p.WEB-DL-GRP", PropagatedLanguages: []string{"en", "fr", "multi"}, OriginalLanguage: "ja"},
			wantAudio: []string{"en", "fr", "multi"},
			wantSubs:  []string{"fr"},
		},
		{
			name:      "bare untagged torrent never guesses original_language",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL.x264-GRP", OriginalLanguage: "en"},
			wantAudio: nil,
			wantSubs:  nil,
		},
		{
			name:      "generic MULTISUB alone stays conservative without inferring audio",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL.MULTISUB.x264-GRP", OriginalLanguage: "en"},
			wantAudio: nil,
			wantSubs:  []string{"multi"},
		},
		{
			name:      "tmdb sentinel xx is ignored",
			rec:       model.AIORecord{Title: "Example.Silent.1920.VOSTFR.1080p.BluRay-GRP", OriginalLanguage: "xx"},
			wantAudio: nil,
			wantSubs:  []string{"fr"},
		},
		{
			name:      "structured unknown/und does not trigger original_language inference",
			rec:       model.AIORecord{Title: "Example.Movie.2024.1080p.WEB-DL-GRP", Languages: []string{"und"}, OriginalLanguage: "en"},
			wantAudio: nil,
			wantSubs:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			audio, subs := streamLanguageEvidence(tc.rec)
			if got := codesOf(audio); !slices.Equal(got, tc.wantAudio) {
				t.Errorf("audio = %v, want %v", got, tc.wantAudio)
			}
			if got := codesOf(subs); !slices.Equal(got, tc.wantSubs) {
				t.Errorf("subs = %v, want %v", got, tc.wantSubs)
			}
		})
	}

	// Verify nativeLanguageEvidence does NOT inject TMDB OriginalLanguage
	// (so cross-source correspondences compare purely torrent-native audio).
	nativeAudio, nativeSubs := nativeLanguageEvidence(model.AIORecord{
		Title:            "Example.Anime.2024.VOSTFR.1080p.WEB-DL-GRP",
		OriginalLanguage: "ja",
	})
	if len(nativeAudio) != 0 || !slices.Equal(codesOf(nativeSubs), []string{"fr"}) {
		t.Fatalf("nativeLanguageEvidence leaked original_language: audio=%v subs=%v", codesOf(nativeAudio), codesOf(nativeSubs))
	}
}

func codesOf(items []streamLanguage) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.code
	}
	return out
}
