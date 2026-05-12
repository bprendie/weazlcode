package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
)

var contextTrimSpinner = spinner.Spinner{
	Frames: []string{
		"▰▱▱▱",
		"▰▰▱▱",
		"▰▰▰▱",
		"▰▰▰▰",
		"▱▰▰▰",
		"▱▱▰▰",
		"▱▱▱▰",
		"▱▱▱▱",
	},
	FPS: time.Second / 12,
}

var modelThinkingPhrases = []string{
	"hacking_the_gibson",
	"jacking_into_the_matrix",
	"breaching_corporate_ice",
	"overclocking_neural_link",
	"tracing_the_uplink",
	"decrypting_sector_7",
	"sniffing_data_packets",
	"bypassing_firewall_01",
	"rerouting_the_mainframe",
	"uploading_virus_payload",
	"accessing_black_ice",
	"mapping_the_grid",
	"ghosting_the_network",
	"prying_open_the_vault",
	"optimizing_cyberdeck",
	"draining_the_data_well",
	"spoofing_host_protocol",
	"syncing_with_the_construct",
	"scrambling_bio_signals",
	"system_reboot_imminent",
	"buuu_ddy",
	"wheezing_the_juice",
	"munching_the_grindage",
	"chilling_the_tokens",
	"chilling_up_on_here",
	"taxing_the_gig",
}
