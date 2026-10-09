// SPDX-License-Identifier: Apache-2.0

package types

import (
	"fmt"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// SetConfigKey sets key to value in the configuration v has read and decodes
// the result into config, a pointer to the file's type. A key no field of
// that type takes is refused: viper accepts any key, and the file would be
// saved without it.
func SetConfigKey(v *viper.Viper, config any, key, value string) error {
	v.Set(key, value)

	var md mapstructure.Metadata
	if err := v.Unmarshal(config, func(c *mapstructure.DecoderConfig) { c.Metadata = &md }); err != nil {
		return err
	}
	// Viper keys are case-insensitive. A key under a table no field takes is
	// reported as the table.
	lower := strings.ToLower(key)
	for _, unused := range md.Unused {
		unused = strings.ToLower(unused)
		if lower == unused || strings.HasPrefix(lower, unused+".") {
			return fmt.Errorf("unknown key %q", key)
		}
	}

	return nil
}
