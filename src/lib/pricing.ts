import type { Price } from '../api/contracts'

export const formatPriceRate = (value: number): string => Number.isFinite(value) ? value.toFixed(4) : '0.0000'

export const isFreePrice = (price: Price): boolean =>
  price.input_per_m === 0 && price.output_per_m === 0 && price.cached_input_per_m === 0 && price.cache_write_per_m === 0

export const priceSummary = (price: Price): string =>
  `$${formatPriceRate(price.input_per_m)} input · $${formatPriceRate(price.output_per_m)} output · $${formatPriceRate(price.cached_input_per_m)} cache read · $${formatPriceRate(price.cache_write_per_m)} cache write / 1M`
