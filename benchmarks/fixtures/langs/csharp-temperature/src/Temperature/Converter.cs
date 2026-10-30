namespace Temperature;

/// <summary>Conversions between Celsius, Fahrenheit and Kelvin.</summary>
public static class Converter
{
    /// <summary>Degrees Fahrenheit for degrees Celsius.</summary>
    public static double CelsiusToFahrenheit(double c) => c * 9 / 5 + 32;

    /// <summary>Degrees Celsius for degrees Fahrenheit.</summary>
    public static double FahrenheitToCelsius(double f) => f - 32 * 5 / 9;

    /// <summary>Kelvin for degrees Celsius; below absolute zero is an error.</summary>
    public static double CelsiusToKelvin(double c)
    {
        if (c < -273.15)
        {
            throw new ArgumentOutOfRangeException(nameof(c), "below absolute zero");
        }
        return c + 273.15;
    }
}
