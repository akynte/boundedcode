using Temperature;
using Xunit;

public class ConverterTests
{
    [Fact]
    public void BoilingPoint() => Assert.Equal(212, Converter.CelsiusToFahrenheit(100), 6);

    [Fact]
    public void AbsoluteZero() => Assert.Equal(0, Converter.CelsiusToKelvin(-273.15), 6);
}
