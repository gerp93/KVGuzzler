import { render } from 'preact';
import './themes/themes.css';
import './styles.css';
import { App } from './App';

render(<App />, document.getElementById('app')!);
