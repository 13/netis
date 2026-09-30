-- Tag colours are palette keys now (slate, indigo, sky, teal, violet, pink,
-- sand) or '' for auto, a hue picked from a hash of the name. Every older
-- value, such as the '#888888' new tags used to get, becomes auto. The
-- column default stays as it was; the store always writes the colour.
UPDATE tag SET color='' WHERE color NOT IN ('slate','indigo','sky','teal','violet','pink','sand');
