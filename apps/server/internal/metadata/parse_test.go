package metadata

import "testing"

func TestParseFilename_AnimeFlatRelease(t *testing.T) {
	info := ParseFilename("[SubsPlease] Frieren - 12 [1080p].mkv")
	if info.Title != "frieren" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.Season != 0 || info.Episode != 0 || info.AbsoluteEpisode != 12 {
		t.Fatalf("unexpected episode info: %+v", info)
	}
}

func TestParseFilename_AnimeSpecial(t *testing.T) {
	info := ParseFilename("[Group] Show OVA 02.mkv")
	if !info.IsSpecial {
		t.Fatal("expected special episode")
	}
	if info.Season != 0 || info.Episode != 2 {
		t.Fatalf("unexpected special mapping: %+v", info)
	}
}

func TestParseFilename_MultiEpisodeRange(t *testing.T) {
	info := ParseFilename("Show.S01E01-E02.mkv")
	if info.Season != 1 || info.Episode != 1 || info.EpisodeEnd != 2 {
		t.Fatalf("unexpected multi-episode parse: %+v", info)
	}
}

func TestParseMovie_CollectionDiscLayout(t *testing.T) {
	info := ParseMovie("Collection/Movie (2010)/Disc 1/movie.mkv", "movie.mkv")
	if info.Title != "Movie" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.Year != 2010 {
		t.Fatalf("year = %d", info.Year)
	}
	if len(info.Collection) != 1 || info.Collection[0] != "Collection" {
		t.Fatalf("collection = %#v", info.Collection)
	}
}

func TestParseMovie_NoisyReleaseFilenameUsesFolderTitle(t *testing.T) {
	info := ParseMovie("Die My Love (2025)/Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4", "Die My Love 2025 BluRay 1080p DD 5 1 x264-BHDStudio.mp4")
	if info.Title != "Die My Love" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.Year != 2025 {
		t.Fatalf("year = %d", info.Year)
	}
}

func TestParseMovie_RemovesReleasePrefixNoise(t *testing.T) {
	info := ParseMovie("[MrManager] Riding Bean (1989) BDRemux (Dual Audio, Special Features).mkv", "[MrManager] Riding Bean (1989) BDRemux (Dual Audio, Special Features).mkv")
	if info.Title != "Riding Bean" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.Year != 1989 {
		t.Fatalf("year = %d", info.Year)
	}
}

func TestParseFilename_StructuredTVNormalizesSeriesName(t *testing.T) {
	info := ParseFilename("Dragon Ball (1986) - S01E01 - Secret of the Dragon Balls [SDTV][AAC 2.0][x265].mkv")
	if info.Title != "dragon ball" {
		t.Fatalf("title = %q", info.Title)
	}
	if info.Season != 1 || info.Episode != 1 {
		t.Fatalf("unexpected episode info: %+v", info)
	}
}

func TestParsePathForMusic_DiscLayout(t *testing.T) {
	info := ParsePathForMusic("Artist/Album/Disc 2/01 - Track.flac", "01 - Track.flac")
	if info.Artist != "Artist" || info.Album != "Album" || info.DiscNumber != 2 {
		t.Fatalf("unexpected music path info: %+v", info)
	}
}
