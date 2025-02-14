import yfinance as yf
import pandas as pd
from datetime import datetime

# User-defined variable for the number of months to check
num_months_to_check = 36

def check_monthly_descending_triangle(data, num_months=num_months_to_check, false_breakout_tolerance=6):
    monthly_data = data.resample('ME').agg({'Close': 'last', 'High': 'max', 'Low': 'min', 'Volume': 'sum'})

    if len(monthly_data) < num_months:
        return False

    triangle_data = monthly_data.tail(num_months)

    highs = triangle_data['High']
    peak = highs.iloc[0]
    false_breakouts = 0
    for i in range(1, len(highs)):
        if highs.iloc[i] > peak:
            false_breakouts += 1
            if false_breakouts > false_breakout_tolerance:
                return False
        else:
            peak = highs.iloc[i]

    high_first, high_last = highs.iloc[0], highs.iloc[-2]
    trendline_slope = (high_last - high_first) / (len(highs) - 1)
    trendline_value = high_first + trendline_slope * (len(highs) - 1)
    if triangle_data['Close'].iloc[-1] <= trendline_value:
        return False

    avg_volume = triangle_data['Volume'].iloc[:-1].mean()
    if triangle_data['Volume'].iloc[-1] <= avg_volume * 1.5:
        return False

    return True

try:
    all_tickers = pd.read_csv('us_stocks.csv')
except FileNotFoundError:
    print("Error: 'us_stocks.csv' file not found.")
    exit(1)
except pd.errors.EmptyDataError:
    print("Error: 'us_stocks.csv' is empty.")
    exit(1)
except Exception as e:
    print(f"Error reading 'us_stocks.csv': {str(e)}")
    exit(1)

processed_count = 0
condition_met_count = 0
results = []

for _, item in all_tickers.iterrows():
    try:
        ticker = item['Symbol'].replace('/', '-')
        data = yf.Ticker(ticker).history(period='5y', interval='1d', auto_adjust=False)

        if data.empty or len(data) < 756 or data['Close'].iloc[-1] <= 5:
            print(f"Insufficient data or low price for {ticker}")
            continue

        if check_monthly_descending_triangle(data):
            results.append({'Ticker': ticker})
            condition_met_count += 1

        processed_count += 1
        print(f"Processed {processed_count} stocks")

    except Exception as e:
        print(f"Error processing {ticker}: {str(e)}")
        continue

if condition_met_count > 0:
    results_df = pd.DataFrame(results)
    filename = f'monthly_descending_triangle_breakouts_{num_months_to_check}m_{datetime.now().strftime("%Y-%m-%d-%H-%M")}.xlsx'
    try:
        results_df.to_excel(filename, index=False)
        print(f"Results written to {filename}")
    except Exception as e:
        print(f"Error writing results to Excel: {str(e)}")
else:
    print("No stocks met the condition. Excel file not created.")

print(f"Total stocks processed: {processed_count}")
print(f"Total stocks meeting condition: {condition_met_count}")
